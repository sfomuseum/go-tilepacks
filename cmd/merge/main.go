package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/maptile"
	"github.com/tilezen/go-tilepacks/tilepack"
)

func pathExists(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return true
}

func main() {
	outputFilename := flag.String("output", "", "The output mbtiles to write to")
	flag.Parse()
	inputFilenames := flag.Args()

	if *outputFilename == "" {
		log.Fatalf("Must specify --output path")
	}

	if len(inputFilenames) == 0 {
		log.Fatalf("Must specify at least one input path")
	}

	log.Printf("Reading %s and writing them to %s", strings.Join(inputFilenames, ", "), *outputFilename)

	// If the output file exists already we shouldn't overwrite it
	if pathExists(*outputFilename) {
		log.Fatalf("Output path %s already exists and cannot be overwritten", *outputFilename)
	}

	var outputBounds orb.Bound
	var outputMinZoom uint
	var outputMaxZoom uint

	inputReaders := make([]tilepack.MbtilesReader, len(inputFilenames))

	for i, inputFilename := range inputFilenames {

		mbtilesReader, err := tilepack.NewMbtilesReader(inputFilename)
		if err != nil {
			log.Fatalf("Couldn't read input mbtiles %s: %+v", inputFilename, err)
		}

		metadata, err := mbtilesReader.Metadata()

		if err != nil {
			log.Fatalf("Unable to read metadata for %s, %v", inputFilename, err)
		}

		bounds, err := metadata.Bounds()

		if err != nil {
			log.Fatalf("Unable to derive bounds for %s, %v", inputFilename, err)
		}

		if i == 0 {
			outputBounds = bounds
		} else {
			outputBounds = outputBounds.Union(bounds)
		}

		minZoom, err := metadata.MinZoom()

		if err != nil {
			log.Fatalf("Unable to min zoom for %s, %v", inputFilename, err)
		}

		maxZoom, err := metadata.MaxZoom()

		if err != nil {
			log.Fatalf("Unable to max zoom for %s, %v", inputFilename, err)
		}

		outputMinZoom = min(outputMinZoom, minZoom)
		outputMaxZoom = min(outputMaxZoom, maxZoom)

		inputReaders[i] = mbtilesReader
	}

	// Create the output mbtiles
	outputMbtiles, err := tilepack.NewMbtilesOutputter(*outputFilename, 1000, outputBounds, maptile.Zoom(outputMinZoom), maptile.Zoom(outputMaxZoom))
	if err != nil {
		log.Fatalf("Couldn't create output mbtiles: %+v", err)
	}

	err = outputMbtiles.CreateTiles()
	if err != nil {
		log.Fatalf("Couldn't create output mbtiles: %+v", err)
	}

	for i, mbtilesReader := range inputReaders {

		err = mbtilesReader.VisitAllTiles(func(t maptile.Tile, data []byte) {
			outputMbtiles.Save(t, data)
		})
		if err != nil {
			log.Fatalf("Couldn't read tiles from %s: %+v", inputFilenames[i], err)
		}
		mbtilesReader.Close()
	}

	outputMbtiles.Close()
}
