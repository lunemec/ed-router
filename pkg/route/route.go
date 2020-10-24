package route

import (
	"fmt"

	"github.com/lunemec/ed-router/pkg/db/boltdb"
	"github.com/lunemec/ed-router/pkg/distance"
	"github.com/lunemec/ed-router/pkg/pather"
	"github.com/lunemec/ed-router/pkg/ship"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	IndexDB  = "index.db"
	GalaxyDB = "galaxy.db"
)

// Route is the main entrypoint for path routing.Route
// expects 2 arguments [from] and [to].
func Route(cmd *cobra.Command, args []string) error {
	db, err := boltdb.Open(IndexDB, GalaxyDB)
	if err != nil {
		return errors.Wrap(err, "unable to open database")
	}

	fromName := args[0]
	toName := args[1]

	if fromName == toName {
		return errors.New("what do you want from me?!")
	}

	ship := ship.New(32, 346.9, 1692.6, 5, 10.5, 878, ship.FSDRating["A"], ship.FSDClass[5])

	p, err := pather.New(db, ship, fromName, toName)
	if err != nil {
		return errors.Wrap(err, "unable to initialize new pather")
	}
	s, err := db.SystemByName("sol")
	if err != nil {
		return errors.Wrap(err, "unable to get sol by name")
	}
	fmt.Printf("%+v \n", s)

	sol, err := db.PointsWithin(0, 0, 0, 0, 0, 0)
	if err != nil {
		return errors.Wrap(err, "unable to get sol")
	}
	fmt.Printf("Sol? %+v \n", sol)
	return nil

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
`, cost)

	var (
		prevSystem *pather.System
		dist       float64
		neutron    = "N"
		refuel     = "N"
	)
	for i, system := range path {
		if prevSystem != nil {
			dist = distance.Distance(prevSystem.Coordinates, system.Coordinates)
		}

		// if system.RefuelAt != nil {
		// 	refuel = fmt.Sprintf("Y [%s (%.1f Ls)]", system.RefuelAt.Name, system.RefuelAt.Distance)
		// }
		// if system.ChargeAt != nil {
		// 	neutron = fmt.Sprintf("Y [%s (%.1f Ls)]", system.ChargeAt.Name, system.ChargeAt.Distance)
		// }

		fmt.Printf("[%d] SUPERCHARGE: %s REFUEL: %s %s (%.1f LY) \n", i, neutron, refuel, system.ID64, dist)
		prevSystem = system
	}
	return nil
}
