package importer

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Songmu/prompter"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v5"
	"github.com/vbauerster/mpb/v5/decor"
	"gonum.org/v1/gonum/spatial/r3"
)

type system struct {
	ID64        int64  `json:"id64"`
	Name        string `json:"name"`
	Coordinates r3.Vec `json:"coords"`
	Bodies      []body `json:"bodies"`
}

type body struct {
	ID64              int64   `json:"id64"`
	Name              string  `json:"name"`
	Type              string  `json:"type"`
	SubType           string  `json:"subType"`
	DistanceToArrival float64 `json:"distanceToArrival"`
}

var defaultDB = "galaxy.db"

// Import is the main entrypoint for path routing.Import
// expects 2 arguments [from] and [to].
func Import(cmd *cobra.Command, args []string) error {
	dumpFile, err := os.Open(args[0])
	if err != nil {
		return errors.Wrap(err, "error openning dump file")
	}
	defer func() {
		err := dumpFile.Close()
		if err != nil {
			fmt.Printf("error closing dump file: %+v \n", err)
		}
	}()

	var dbFile = defaultDB
	if info, err := os.Stat(dbFile); !os.IsNotExist(err) && !info.IsDir() {
		var rmDB bool = prompter.YesNo("Database file exists, replace?", false)
		if rmDB {
			err = os.Remove(dbFile)
			if err != nil {
				return errors.Wrapf(err, "error deleting DB file by hand, please remove it: %s", dbFile)
			}
		}
	}

	db, err := newSQLiteDB(dbFile)
	if err != nil {
		return errors.Wrap(err, "unable to open DB")
	}
	p, proxyReader, err := progressBar(dumpFile)
	defer p.Wait()
	defer proxyReader.Close()
	if err != nil {
		return errors.Wrap(err, "error initializing progress bar")
	}

	gzReader, err := gzip.NewReader(proxyReader)
	defer gzReader.Close()
	if err != nil {
		return errors.Wrap(err, "unable to decompress dump file")
	}

	conf := jsoniter.ConfigCompatibleWithStandardLibrary
	iter := jsoniter.Parse(conf, gzReader, 10240)
	defer conf.ReturnIterator(iter)

	for iter.ReadArray() {
		var system system
		iter.ReadVal(&system)
		if iter.Error != nil {
			return errors.Wrap(err, "error decoding system")
		}
		err = db.insertSystem(&system)
		if err != nil {
			return errors.Wrap(err, "error inserting system to DB")
		}
	}

	return nil
}

func progressBar(dumpFile *os.File) (*mpb.Progress, io.ReadCloser, error) {
	dumpFileStat, err := dumpFile.Stat()
	if err != nil {
		return nil, nil, errors.Wrap(err, "unable to stat dump file")
	}
	p := mpb.New(
		mpb.WithRefreshRate(180 * time.Millisecond),
	)
	bar := p.AddBar(dumpFileStat.Size(),
		mpb.BarStyle("[=>-|"),
		mpb.PrependDecorators(
			decor.CountersKibiByte("% .2f / % .2f"),
		),
		mpb.AppendDecorators(
			decor.EwmaETA(decor.ET_STYLE_GO, 90),
			decor.Name(" ] "),
			decor.EwmaSpeed(decor.UnitKiB, "% .2f", 60),
		))
	return p, bar.ProxyReader(dumpFile), nil
}
