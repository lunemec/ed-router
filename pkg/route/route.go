package route

import (
	"fmt"

	"github.com/lunemec/ed-router/pkg/db/sqlite"
	"github.com/lunemec/ed-router/pkg/importer"
	"github.com/lunemec/ed-router/pkg/pather"

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

	pather, err := pather.New(db, fromName, toName)
	if err != nil {
		return errors.Wrap(err, "unable to initialize new pather")
	}

	from := pather.From()
	to := pather.To()

	fmt.Printf(`
Start: %s at %+v
End: %s at %+v
Distance: %f
`, from.Name, from.Coordinates, to.Name, to.Coordinates, pather.Distance())

	path, cost, found := pather.Path()
	if !found {
		fmt.Println("No path found.")
		return nil
	}
	fmt.Printf(`
Found path with cost: %f
`, cost)
	for i, system := range path {
		fmt.Printf("[%d] %s %+v \n", i, system.Name, system.Coordinates)
	}
	return nil
}
