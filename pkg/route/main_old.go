package route

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/beefsack/go-astar"
	jsoniter "github.com/json-iterator/go"
	"github.com/panjf2000/ants/v2"
	"github.com/pkg/errors"
	"gopkg.in/tomb.v2"
)

const (
	systemNameFrom   = "Shinrarta Dezhra"
	systemNameTo     = "Sol"
	shipJumpRange    = 10 //67.37 // Should we use Laden or Max? TODO calculate fuel consumption
	edsmDumpFilePath = "systemsWithCoordinates.json"
)

const (
	cylinderFilterRadius = shipJumpRange * 1 // in LY
)

// https://www.analytics-link.com/post/2018/09/18/applying-the-a-path-finding-algorithm-in-python-part-3-3d-coordinate-pairs
func XXX() {
	start := time.Now()
	defer func() {
		fmt.Printf("Finished in %s", time.Since(start))
	}()
	err := run()
	if err != nil {
		fmt.Printf("%+v", err)
	}
}

func run() error {
	fmt.Printf("Finding shortest path from %s -> %s \n", systemNameFrom, systemNameTo)
	var (
		start, end *system
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	systemsChan := streamSystems(ctx, edsmDumpFilePath)
	// Start by finding FROM/TO systems before we continue.
	for system := range systemsChan {
		if start != nil && end != nil {
			cancel()
			break
		}
		system := system
		if system.Name == systemNameFrom {
			start = &system
		}
		if system.Name == systemNameTo {
			end = &system
		}
	}

	if start == nil {
		return errors.Errorf("Starting system %s not found.", systemNameFrom)
	}
	if end == nil {
		return errors.Errorf("Target system %s not found.", systemNameTo)
	}
	totalDistance := distance(start.Coordinates, end.Coordinates)
	if totalDistance <= shipJumpRange {
		fmt.Printf("System is within range. \n")
		return nil
	}

	fmt.Printf(`
Start: %s at %+v
End: %s at %+v
Distance: %f
`, start.Name, start.Coordinates, end.Name, end.Coordinates, totalDistance)

	// Find all systems that lie within cylinder between
	// start and end systems, having radius cylinderFilterRadius.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("Filtering systems to be checked.\n")
	_, err := systemsToRoute(ctx, start, end)
	if err != nil {
		return errors.Wrap(err, "error filtering systems")
	}

	fmt.Printf("Searching for path.\n")
	path, distance, found := astar.Path(start, end)
	if !found {
		fmt.Printf("No viable path found. \n")
		return nil
	}
	fmt.Printf(`
Path found.
Distance: %+v
`, distance)

	for i, pathSystem := range path {
		s := pathSystem.(*system)
		fmt.Printf("[%d] %s %+v \n", i, s.Name, s.Coordinates)
	}
	return nil
}

func streamSystems(ctx context.Context, filepath string) chan system {
	var (
		tomb, _     = tomb.WithContext(ctx)
		systemsChan = make(chan system)
	)

	tomb.Go(func() error {
		edsmDumpFile, err := os.Open(filepath)
		if err != nil {
			panic(err)
		}
		defer func() {
			err := edsmDumpFile.Close()
			if err != nil {
				fmt.Printf("error closing dump file")
			}
		}()

		iter := jsoniter.Parse(jsoniter.ConfigCompatibleWithStandardLibrary, edsmDumpFile, 1024)
		defer jsoniter.ConfigCompatibleWithStandardLibrary.ReturnIterator(iter)

		for iter.ReadArray() {
			var system system
			iter.ReadVal(&system)
			if iter.Error != nil {
				return errors.Wrap(err, "error decoding system")
			}
			select {
			case systemsChan <- system:
			case <-tomb.Dying():
				close(systemsChan)
				return nil
			}
		}

		close(systemsChan)
		return nil
	})

	return systemsChan
}

// systemsToRoute filters systems to consider when path finding, to increase speed and memory cost.
// It checks every system coordinates if it lies within cylinder between start/end systems
// with radius "cylinderFilterRadius".
func systemsToRoute(ctx context.Context, start, end *system) ([]system, error) {
	var (
		searchSystems   []system
		filteredSystems = make(chan system)
		wg              sync.WaitGroup
		workers         = 160
	)

	systemsChan := streamSystems(ctx, edsmDumpFilePath)
	pool, err := ants.NewPool(workers, ants.WithNonblocking(true))
	if err != nil {
		return nil, errors.Wrap(err, "error spawning goroutine pool")
	}
	defer pool.Release()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		err := pool.Submit(func() {
			defer wg.Done()
		LOOP:
			for {
				select {
				case system, ok := <-systemsChan:
					if !ok {
						break LOOP
					}
					if isInCylinder(start.Coordinates, end.Coordinates, cylinderFilterRadius, system.Coordinates) {
						filteredSystems <- system
					}
				case <-ctx.Done():
					break LOOP
				}
			}
		})
		if err != nil {
			return nil, errors.Wrap(err, "error submitting worker")
		}
	}
	go func() {
		wg.Wait()
		close(filteredSystems)
	}()

	for filteredSystem := range filteredSystems {
		searchSystems = append(searchSystems, filteredSystem)
	}

	fmt.Printf("Systems to check: %d\n", len(searchSystems))
	return searchSystems, nil
}
