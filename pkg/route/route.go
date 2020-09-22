package route

import (
	"fmt"

	"github.com/lunemec/ed-router/pkg/db/sqlite"
	"github.com/lunemec/ed-router/pkg/distance"
	"github.com/lunemec/ed-router/pkg/importer"
	"github.com/lunemec/ed-router/pkg/pather"
	"github.com/lunemec/ed-router/pkg/ship"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// Route is the main entrypoint for path routing.Route
// expects 2 arguments [from] and [to].
func Route(cmd *cobra.Command, args []string) error {
	dbFile := importer.DefaultDB
	db, err := sqlite.Open(dbFile)
	if err != nil {
		return errors.Wrapf(err, "unable to open database %s", dbFile)
	}
	fromName := args[0]
	toName := args[1]

	if fromName == toName {
		return errors.New("what do you want from me?!")
	}

	ship := ship.New(72.52, 32, 878, ship.FSDRating["A"], ship.FSDClass[5])

	p, err := pather.New(db, ship, fromName, toName)
	if err != nil {
		return errors.Wrap(err, "unable to initialize new pather")
	}

	from := p.From()
	to := p.To()

	fmt.Printf(`
Start: %s at %+v
End: %s at %+v
Distance: %f LY
Estimated cost (time): %fs
`, from.Name, from.Coordinates, to.Name, to.Coordinates, p.Distance(), from.PathEstimatedCost(to))

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

		if system.RefuelAt != nil {
			refuel = fmt.Sprintf("Y [%s (%f)]", system.RefuelAt.Name, system.RefuelAt.Distance)
		}
		if system.ChargeAt != nil {
			neutron = fmt.Sprintf("Y [%s (%f)]", system.ChargeAt.Name, system.ChargeAt.Distance)
		}

		fmt.Printf("[%d] SUPERCHARGE: %s REFUEL: %s %s (%f LY) \n", i, neutron, refuel, system.Name, dist)
		prevSystem = system
	}
	return nil
}
