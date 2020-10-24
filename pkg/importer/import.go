package importer

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/lunemec/ed-router/pkg/db/boltdb"
	"github.com/lunemec/ed-router/pkg/models/dump"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v5"
	"github.com/vbauerster/mpb/v5/decor"
)

var (
	IndexDB  = "index.db"
	GalaxyDB = "galaxy.db"
)

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

	db, err := boltdb.Open(IndexDB, GalaxyDB)
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
		var system dump.System
		iter.ReadVal(&system)
		if iter.Error != nil {
			return errors.Wrap(err, "error decoding system")
		}
		err = db.InsertSystem(system)
		if err != nil {
			return errors.Wrap(err, "error inserting system to DB")
		}
	}
	db.StopInsert()

	return db.Close()
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
