package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Add constants for better maintainability
const (
	pokedexURL = "https://pokemondb.net/pokedex/all"
	baseExpURL = "https://bulbapedia.bulbagarden.net/wiki/List_of_Pokémon_by_effort_value_yield_(Generation_IX)"
	pokedexFile = "pokedex.json"
)

type Attributes struct {
	HP           int `json:"hp"`
	Attack       int `json:"attack"`
	Defense      int `json:"defense"`
	Speed        int `json:"speed"`
	SpAttack     int `json:"sp_attack"`
	SpDefense    int `json:"sp_defense"`
	DmgWhenAtked int `json:"dmg_when_atked"`
}

type Pokemon struct {
	Name       string     `json:"name"`
	Type       []string   `json:"type"`
	BaseExp    int        `json:"base_exp"`
	Experience int        `json:"exp"`
	Level      int        `json:"level"`
	EV         float64    `json:"ev"`
	Attributes Attributes `json:"attributes"`
}

type Pokedex struct {
	Pokemons []Pokemon `json:"pokemons"`
}

// Use context for HTTP requests
func fetchPokemonData(ctx context.Context) ([]Pokemon, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pokedexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("status code: %d %s", res.StatusCode, res.Status)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	var pokemons []Pokemon
	// Find each Pokemon row in the table
	doc.Find("table.data-table tbody tr").Each(func(i int, s *goquery.Selection) {
		// Initialize a Pokemon struct
		pokemon := Pokemon{}

		// Find and parse Pokemon name
		pokemon.Name = s.Find("td.cell-name a.ent-name").Text()

		// Find and parse Pokemon types
		typeSelection := s.Find("td.cell-icon a")
		typeSelection.Each(func(j int, typeLink *goquery.Selection) {
			pokemon.Type = append(pokemon.Type, strings.TrimSpace(typeLink.Text()))
		})

		// Initialize Attributes
		pokemon.BaseExp = 0
		pokemon.Experience = 0
		pokemon.Level = 1
		pokemon.EV = 0.5

		// Find and parse Pokemon attributes
		s.Find("td.cell-num").Each(func(k int, attrSelection *goquery.Selection) {
			attrValue := parseIntOrDefault(attrSelection.Text(), 0)
			// Assign attribute value to the corresponding field
			switch k {
			case 2:
				pokemon.Attributes.HP = attrValue
			case 3:
				pokemon.Attributes.Attack = attrValue
			case 4:
				pokemon.Attributes.Defense = attrValue
			case 5:
				pokemon.Attributes.SpAttack = attrValue
			case 6:
				pokemon.Attributes.SpDefense = attrValue
			case 7:
				pokemon.Attributes.Speed = attrValue
			}
		})

		// Append Pokemon data to the slice
		pokemons = append(pokemons, pokemon)
	})

	return pokemons, nil
}

func fetchBaseExp(pokemons []Pokemon) error {
	// Fetch the HTML page
	doc, err := goquery.NewDocument(baseExpURL)
	if err != nil {
		return fmt.Errorf("failed to fetch HTML: %v", err)
	}

	// Map to store BaseExp values by Pokemon name for quick lookup
	baseExpMap := make(map[string]int)

	// Find each Pokemon row in the table
	doc.Find("table.sortable tbody tr").Each(func(i int, s *goquery.Selection) {
		// Find Pokemon name
		pokemonName := strings.TrimSpace(s.Find("td").Eq(2).Find("a").Text())

		// Find and parse BaseExp
		baseExpStr := strings.TrimSpace(s.Find("td").Eq(3).Text())
		baseExp, err := strconv.Atoi(baseExpStr) // Convert string to int
		if err != nil {
			log.Printf("Error parsing BaseExp for Pokemon: %s - %v", pokemonName, err)
			return
		}

		// Store BaseExp in the map
		baseExpMap[pokemonName] = baseExp
	})

	// Assign BaseExp to corresponding Pokemon structs
	for i := range pokemons {
		if baseExp, ok := baseExpMap[pokemons[i].Name]; ok {
			pokemons[i].BaseExp = baseExp
		}
	}

	return nil
}

// Save pokedex to a JSON file
func savePokedex(pokedex Pokedex) error {
	f, err := os.Create(pokedexFile)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	if err := enc.Encode(pokedex.Pokemons); err != nil {
		return fmt.Errorf("encoding pokedex: %w", err)
	}

	return w.Flush()
}

// Load pokedex from a JSON file
func loadPokedex() (Pokedex, error) {
	data, err := ioutil.ReadFile(pokedexFile)
	if err != nil {
		return Pokedex{}, err
	}
	var pokedex Pokedex
	err = json.Unmarshal(data, &pokedex.Pokemons)
	return pokedex, err
}

func levelUp(pokemon *Pokemon) {
	// Calculate required experience points for the next level
	requiredExp := pokemon.BaseExp * (1 << (pokemon.Level - 1))

	// Check if the Pokemon has enough experience points to level up
	if pokemon.Experience >= requiredExp {
		pokemon.Level++
		pokemon.Experience -= requiredExp // Deduct required experience points
		evMultiplier := 1.0 + pokemon.EV  // EV multiplier

		// Increase attributes based on the EV multiplier
		pokemon.Attributes.HP = int(float64(pokemon.Attributes.HP) * evMultiplier)
		pokemon.Attributes.Attack = int(float64(pokemon.Attributes.Attack) * evMultiplier)
		pokemon.Attributes.Defense = int(float64(pokemon.Attributes.Defense) * evMultiplier)
		pokemon.Attributes.SpAttack = int(float64(pokemon.Attributes.SpAttack) * evMultiplier)
		pokemon.Attributes.SpDefense = int(float64(pokemon.Attributes.SpDefense) * evMultiplier)
	}
}

// Add error handling helper
func parseAttribute(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

// Optimize attribute parsing
func (p *Pokemon) parseAttributes(s *goquery.Selection) {
	attrs := s.Find("td.cell-num")
	if attrs.Length() >= 8 {
		p.Attributes = Attributes{
			HP:        parseIntOrDefault(attrs.Eq(2).Text(), 0),
			Attack:    parseIntOrDefault(attrs.Eq(3).Text(), 0),
			Defense:   parseIntOrDefault(attrs.Eq(4).Text(), 0),
			SpAttack: parseIntOrDefault(attrs.Eq(5).Text(), 0),
			SpDefense: parseIntOrDefault(attrs.Eq(6).Text(), 0),
			Speed:     parseIntOrDefault(attrs.Eq(7).Text(), 0),
		}
	}
}

func parseIntOrDefault(s string, defaultVal int) int {
	val, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return defaultVal
	}
	return val
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Fetch and save pokedex
	pokemons, err := fetchPokemonData(context.Background())
	if err != nil {
		fmt.Println("Error fetching pokemon data:", err)
		return
	}

	err = fetchBaseExp(pokemons)
	if err != nil {
		fmt.Println("Error fetching base exp data:", err)
		return
	}

	pokedex := Pokedex{Pokemons: pokemons}
	err = savePokedex(pokedex)
	if err != nil {
		fmt.Println("Error saving pokedex:", err)
		return
	}

	// Load pokedex
	pokedex, err = loadPokedex()
	if err != nil {
		fmt.Println("Error loading pokedex:", err)
		return
	}

	// Save updated pokedex
	err = savePokedex(pokedex)
	if err != nil {
		fmt.Println("Error saving updated pokedex:", err)
		return
	}

	fmt.Println("Pokedex saved successfully!")
}
