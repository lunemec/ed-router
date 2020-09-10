package main

import (
	"fmt"
	"sync"

	"github.com/beefsack/go-astar"
	"github.com/panjf2000/ants/v2"
	"gonum.org/v1/gonum/spatial/r3"
)

type system struct {
	//ID          int    `json:"id,omitempty"`
	//ID64        int64  `json:"id64,omitempty"`
	Name        string `json:"name,omitempty"`
	Coordinates r3.Vec `json:"coords,omitempty"` // coordinates in the dump mean distance in LY from Sol, which is 0,0,0.
	systems     []system
}

func (c *system) PathNeighbors() []astar.Pather {
	var (
		neighbors []astar.Pather
		shipRange = shipJumpRange * c.ChargeMultiplier()

		neighborsChan = make(chan *system)

		wg sync.WaitGroup
	)

	pool, err := ants.NewPool(16)
	if err != nil {
		panic(err)
	}

	wg.Add(len(c.systems))
	go func() {
		for _, system := range c.systems {
			err = pool.Submit(func() {
				defer wg.Done()
				// If system is within range of this system.
				// TODO calculate fuel here or in the cost function?
				if distance(c.Coordinates, system.Coordinates) <= shipRange {
					neighborsChan <- &system
				}
			})
			if err != nil {
				panic(err)
			}
		}
	}()

	go func() {
		wg.Wait()
		close(neighborsChan)
	}()

	for neighbor := range neighborsChan {
		neighbors = append(neighbors, neighbor)
	}
	fmt.Printf("%s: %d neighbors \n", c.Name, len(neighbors))
	return neighbors
}

func (c *system) PathNeighborCost(to astar.Pather) float64 {
	shipRange := shipJumpRange * c.ChargeMultiplier()
	// Prefer longer jumps over shorter.
	cost := shipRange / distance(c.Coordinates, to.(*system).Coordinates)
	// TODO cost function, fuel, star type, prefer neutrons, etc.
	return cost
}

func (c *system) PathEstimatedCost(to astar.Pather) float64 {
	return c.ManhattanDistance(to.(*system))
}

func (c *system) ManhattanDistance(to *system) float64 {
	return manhattanDistance(c.Coordinates, to.Coordinates)
}

func (c *system) ChargeMultiplier() float64 {
	// TODO add Star(s) type(s) and decide based on that.
	return 1
}
