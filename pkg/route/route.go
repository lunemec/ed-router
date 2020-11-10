package route

import (
	"fmt"

	"github.com/lunemec/ed-router/pkg/db/boltdb"
	"github.com/lunemec/ed-router/pkg/distance"
	"github.com/lunemec/ed-router/pkg/pather"
	"github.com/lunemec/ed-router/pkg/ship"

	"github.com/pkg/errors"
	"github.com/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	IndexDB  = "index_xyz.db"
	GalaxyDB = "galaxy.db"
)

// Route is the main entrypoint for path routing.Route
// expects 2 arguments [from] and [to].
func Route(cmd *cobra.Command, args []string) error {
	defer profile.Start().Stop()

	db, err := boltdb.Open(IndexDB, GalaxyDB, true)
	if err != nil {
		return errors.Wrap(err, "unable to open database")
	}

	fromName := args[0]
	toName := args[1]

	if fromName == toName {
		return errors.New("what do you want from me?!")
	}

	ship := ship.New(32, 346.9, 1692.6, 5, 10.5, 878, ship.FSDRating["A"], ship.FSDClass[5])
	fmt.Printf("Jump Range: %f \n", ship.JumpRange())
	p, err := pather.New(db, ship, fromName, toName)
	if err != nil {
		return errors.Wrap(err, "unable to initialize new pather")
	}

	from := p.From()
	to := p.To()

	fmt.Printf(`
Start: %d at %+v
End: %d at %+v
Distance: %.1f LY
`, from.ID64, from.Coordinates, to.ID64, to.Coordinates, p.Distance())

	path, cost, found := p.Path()
	if !found {
		fmt.Println("No path found.")
		return nil
	}
	fmt.Printf(`
Found path with cost: %f
Systems checked: %d
`, cost, p.Stats())

	var (
		prevSystem      *pather.System
		dist            float64
		neutron, refuel string
	)
	for i, system := range path {
		if prevSystem != nil {
			dist = distance.Distance(prevSystem.Coordinates, system.Coordinates)
		}

		if system.Neutron {
			neutron = "Y"
		} else {
			neutron = "N"
		}

		if system.Scoopable {
			refuel = "Y"
		} else {
			refuel = "N"
		}

		fullSystem, err := db.SystemByID(system.ID64)
		if err != nil {
			fmt.Printf("%+v \n", err)
		}

		fmt.Printf("[%d] SUPERCHARGE: %s REFUEL: %s %s (%.1f LY) \n", i, neutron, refuel, fullSystem.Name, dist)
		prevSystem = system
	}
	return nil
}
