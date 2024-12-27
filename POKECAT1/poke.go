package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
	"github.com/gorilla/websocket"
)

// Cell represents each cell on the map, potentially containing a Pokémon
type Cell struct {
	Pokemon *Pokemon
}

// Pokemon represents a Pokémon with its attributes
type Pokemon struct {
	Name      string    `json:"name"`
	Level     int       `json:"level"`
	EV        float64   `json:"ev"`
	SpawnedAt time.Time
}

// Player represents a player in the game
type Player struct {
	ID       int
	X, Y     int
	Captured []*Pokemon
}

const (
	MapSize          = 20
	PokemonPerWave   = 50
	PokemonLifetime  = 5 * time.Minute
	MaxPokemons      = 200
)

var (
	world    [MapSize][MapSize]Cell
	player   *Player
	lock     sync.Mutex
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	globalMutex sync.Mutex
	pokemonData []Pokemon
)

// loadPokemonData loads Pokémon data from a JSON file
func loadPokemonData() {
	data, err := ioutil.ReadFile("pokemon.json")
	if err != nil {
		log.Fatalf("Failed to read pokemon.json: %v", err)
	}

	if err := json.Unmarshal(data, &pokemonData); err != nil {
		log.Fatalf("Failed to parse pokemon.json: %v", err)
	}

	if len(pokemonData) > 50 {
		pokemonData = pokemonData[:50]
	}

	fmt.Printf("Loaded %d Pokémon from json\n", len(pokemonData))
}

// spawnPokemon spawns new Pokémon on the map
func spawnPokemon() {
	lock.Lock()
	defer lock.Unlock()

	for i := 0; i < PokemonPerWave; i++ {
		if len(pokemonData) == 0 {
			fmt.Println("No Pokémon data available to spawn.")
			return
		}

		pokemon := pokemonData[rand.Intn(len(pokemonData))]
		level := rand.Intn(100) + 1
		ev := 0.5 + rand.Float64()*(1.0-0.5)
		x, y := rand.Intn(MapSize), rand.Intn(MapSize)

		world[x][y].Pokemon = &Pokemon{
			Name:      pokemon.Name,
			Level:     level,
			EV:        ev,
			SpawnedAt: time.Now(),
		}
	}
}

// despawnPokemon removes Pokémon that have exceeded their lifetime
func despawnPokemon() {
	lock.Lock()
	defer lock.Unlock()

	now := time.Now()
	for x := 0; x < MapSize; x++ {
		for y := 0; y < MapSize; y++ {
			if world[x][y].Pokemon != nil && now.Sub(world[x][y].Pokemon.SpawnedAt) > PokemonLifetime {
				world[x][y].Pokemon = nil
			}
		}
	}
}

// addPlayer adds a new player to the game
func addPlayer() (*Player, error) {
	lock.Lock()
	defer lock.Unlock()

	if player != nil {
		return nil, fmt.Errorf("a player is already in the game")
	}

	player = &Player{
		ID: rand.Int(),
		X:  rand.Intn(MapSize),
		Y:  rand.Intn(MapSize),
	}
	return player, nil
}

// capturePokemon allows a player to capture a Pokémon
func capturePokemon(p *Player) {
	if p.X < 0 || p.X >= MapSize || p.Y < 0 || p.Y >= MapSize {
		fmt.Println("Player is out of bounds, cannot capture Pokémon.")
		return
	}

	globalMutex.Lock()
	defer globalMutex.Unlock()

	cell := &world[p.X][p.Y]
	for cell.Pokemon != nil && len(p.Captured) < MaxPokemons {
		p.Captured = append(p.Captured, cell.Pokemon)
		pokemon := cell.Pokemon
		cell.Pokemon = nil

		fmt.Printf("Player %d captured a Pokémon: %v at position X = %d, Y = %d\n", p.ID, pokemon.Name, p.X, p.Y)
		fmt.Printf("Captured Pokémon Details:\n- Name: %s\n- Level: %d\n- EV: %.2f\n", pokemon.Name, pokemon.Level, pokemon.EV)

		cell = &world[p.X][p.Y]
	}
	if cell.Pokemon == nil {
		fmt.Printf("No Pokémon to capture at position X = %d, Y = %d\n", p.X, p.Y)
	}

	fmt.Println("Captured Pokémon:")
	for _, pokemon := range p.Captured {
		fmt.Printf("- %s\n", pokemon.Name)
	}
}

// wsHandler handles WebSocket connections
func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Error upgrading connection:", err)
		return
	}

	p, err := addPlayer()
	if err != nil {
		conn.WriteJSON(map[string]string{"error": "Only one player allowed"})
		fmt.Println(err)
		conn.Close()
		return
	}

	fmt.Printf("Player %d connected\n", p.ID)

	defer func() {
		lock.Lock()
		player = nil
		lock.Unlock()
		fmt.Printf("Player %d disconnected\n", p.ID)
	}()

	gameLoop(conn, p)
}

// gameLoop processes the game loop for a player
func gameLoop(conn *websocket.Conn, p *Player) {
	for {
		data := map[string]interface{}{
			"player": map[string]int{"X": p.X, "Y": p.Y},
			"world":  world,
		}
		if err := conn.WriteJSON(data); err != nil {
			fmt.Println("Error writing to websocket:", err)
			return
		}

		var move map[string]interface{}
		if err := conn.ReadJSON(&move); err != nil {
			fmt.Println("Error reading from websocket:", err)
			return
		}

		fmt.Printf("Received JSON: %v\n", move)

		if capture, ok := move["capture"].(bool); ok && capture {
			fmt.Println("Attempting to capture a Pokémon!")
			capturePokemon(p)
			continue
		}

		if dx, dxOk := move["dx"].(float64); dxOk {
			if p.X+int(dx) >= 0 && p.X+int(dx) < MapSize {
				p.X += int(dx)
			}
		}
		if dy, dyOk := move["dy"].(float64); dyOk {
			if p.Y+int(dy) >= 0 && p.Y+int(dy) < MapSize {
				p.Y += int(dy)
			}
		}
		fmt.Printf("Player new position: X = %d, Y = %d\n", p.X, p.Y)

		capturePokemon(p)
	}
}

// main initializes the server and game logic
func main() {
	rand.Seed(time.Now().UnixNano())
	loadPokemonData()

	go func() {
		for {
			spawnPokemon()
			time.Sleep(1 * time.Minute)
		}
	}()

	go func() {
		for {
			despawnPokemon()
			time.Sleep(10 * time.Second)
		}
	}()

	http.HandleFunc("/ws", wsHandler)
	fmt.Println("Server started at :8080")
	http.ListenAndServe(":8080", nil)
}
