package main

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tilezen/go-tilepacks/tilepack"
)

type TileRequest struct {
	Tile *tilepack.Tile
	URL  string
	Gzip bool
}

type TileResponse struct {
	Tile    *tilepack.Tile
	Data    []byte
	Elapsed float64
}

const (
	httpUserAgent   = "go-tilepacks/1.0"
	saveLogInterval = 10000
)

func doHTTPWithRetry(client *http.Client, request *http.Request, nRetries int) (*http.Response, error) {
	sleep := 500 * time.Millisecond

	for i := 0; i < nRetries; i++ {
		resp, err := client.Do(request)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 200 {
			return resp, nil
		}

		// log.Printf("Failed to GET (try %d) %+v: %+v", i, request.URL, resp.Status)
		if resp.StatusCode > 500 && resp.StatusCode < 600 {
			time.Sleep(sleep)
			sleep *= 2.0
			if sleep > 30.0 {
				sleep = 30 * time.Second
			}
		}
	}

	return nil, fmt.Errorf("ran out of HTTP GET retries for %s", request.URL)
}

func httpWorker(wg *sync.WaitGroup, id int, client *http.Client, jobs chan *TileRequest, results chan *TileResponse) {
	defer wg.Done()

	// Instantiate the gzip support stuff once instead on every iteration
	bodyBuffer := bytes.NewBuffer(nil)
	bodyGzipper := gzip.NewWriter(bodyBuffer)

	for request := range jobs {
		start := time.Now()

		httpReq, err := http.NewRequest("GET", request.URL, nil)
		if err != nil {
			log.Printf("Unable to create HTTP request: %+v", err)
			continue
		}

		httpReq.Header.Add("User-Agent", httpUserAgent)
		if request.Gzip {
			httpReq.Header.Add("Accept-Encoding", "gzip")
		}

		resp, err := doHTTPWithRetry(client, httpReq, 30)
		if err != nil {
			log.Printf("Skipping %+v: %+v", request, err)
			continue
		}

		var bodyData []byte
		if request.Gzip {
			contentEncoding := resp.Header.Get("Content-Encoding")
			if contentEncoding == "gzip" {
				bodyData, err = ioutil.ReadAll(resp.Body)
			} else {
				// Reset at the top in case we ran into a continue below
				bodyBuffer.Reset()
				bodyGzipper.Reset(bodyBuffer)

				_, err = io.Copy(bodyGzipper, resp.Body)
				if err != nil {
					log.Printf("Couldn't copy to gzipper: %+v", err)
					continue
				}

				err = bodyGzipper.Flush()
				if err != nil {
					log.Printf("Couldn't flush gzipper: %+v", err)
					continue
				}

				bodyData, err = ioutil.ReadAll(bodyBuffer)
				if err != nil {
					log.Printf("Couldn't read bytes into byte array: %+v", err)
					continue
				}
			}
		} else {
			bodyData, err = ioutil.ReadAll(resp.Body)
		}
		resp.Body.Close()

		if err != nil {
			log.Printf("Error copying bytes from HTTP response: %+v", err)
			continue
		}

		secs := time.Since(start).Seconds()

		results <- &TileResponse{
			Tile:    request.Tile,
			Data:    bodyData,
			Elapsed: secs,
		}

		// Sleep a tiny bit to try to prevent thundering herd
		time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
	}
}

func processResults(waitGroup *sync.WaitGroup, results chan *TileResponse, processor tilepack.TileOutputter) {
	defer waitGroup.Done()

	start := time.Now()

	counter := 0
	for result := range results {
		err := processor.Save(result.Tile, result.Data)
		if err != nil {
			log.Printf("Couldn't save tile %+v", err)
		}

		counter++

		if counter%saveLogInterval == 0 {
			duration := time.Since(start)
			start = time.Now()
			log.Printf("Saved %dk tiles (%0.1f tiles per second)", counter/1000, saveLogInterval/duration.Seconds())
		}
	}
	log.Printf("Saved %d tiles", counter)

	err := processor.Close()
	if err != nil {
		log.Printf("Error closing processor: %+v", err)
	}
}

func main() {
	urlTemplateStr := flag.String("url", "", "URL template to make tile requests with.")
	outputMode := flag.String("mode", "mbtiles", "Valid modes are: disk, mbtiles.")
	outputDSN := flag.String("dsn", "", "Path, or DSN string, to output files.")	
	boundingBoxStr := flag.String("bounds", "-90.0,-180.0,90.0,180.0", "Comma-separated bounding box in south,west,north,east format. Defaults to the whole world.")
	zoomsStr := flag.String("zooms", "0,1,2,3,4,5,6,7,8,9,10", "Comma-separated list of zoom levels.")
	numHTTPWorkers := flag.Int("workers", 25, "Number of HTTP client workers to use.")
	gzipEnabled := flag.Bool("gzip", false, "Request gzip encoding from server and store gzipped contents in mbtiles. Will gzip locally if server doesn't do it.")
	requestTimeout := flag.Int("timeout", 60, "HTTP client timeout for tile requests.")
	cpuProfile := flag.String("cpuprofile", "", "Enables CPU profiling. Saves the dump to the given path.")
	invertedY := flag.Bool("inverted-y", false, "Invert the Y-value of tiles to match the TMS (as opposed to ZXY) tile format.")
	flag.Parse()

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	if *outputDSN == "" {
		log.Fatalf("Output DSN (-dsn) is required")
	}

	if *urlTemplateStr == "" {
		log.Fatalf("URL template is required")
	}

	boundingBoxStrSplit := strings.Split(*boundingBoxStr, ",")
	if len(boundingBoxStrSplit) != 4 {
		log.Fatalf("Bounding box string must be a comma-separated list of 4 numbers")
	}

	boundingBoxFloats := make([]float64, 4)
	for i, bboxStr := range boundingBoxStrSplit {
		bboxFloat, err := strconv.ParseFloat(bboxStr, 64)
		if err != nil {
			log.Fatalf("Bounding box string could not be parsed as numbers")
		}

		boundingBoxFloats[i] = bboxFloat
	}

	bounds := &tilepack.LngLatBbox{
		South: boundingBoxFloats[0],
		West:  boundingBoxFloats[1],
		North: boundingBoxFloats[2],
		East:  boundingBoxFloats[3],
	}

	zoomsStrSplit := strings.Split(*zoomsStr, ",")
	zooms := make([]uint, len(zoomsStrSplit))
	for i, zoomStr := range zoomsStrSplit {
		z, err := strconv.ParseUint(zoomStr, 10, 32)
		if err != nil {
			log.Fatalf("Zoom list could not be parsed: %+v", err)
		}

		zooms[i] = uint(z)
	}

	// Configure the HTTP client with a timeout and connection pools
	httpClient := &http.Client{}
	httpClient.Timeout = time.Duration(*requestTimeout) * time.Second
	httpTransport := &http.Transport{
		MaxIdleConnsPerHost: 500,
		DisableCompression:  true,
	}
	httpClient.Transport = httpTransport

	var outputter tilepack.TileOutputter
	var outputter_err error

	switch *outputMode {
	case "disk":
		outputter, outputter_err = tilepack.NewDiskOutputter(*outputDSN)
	case "mbtiles":
		outputter, outputter_err = tilepack.NewMbtilesOutputter(*outputDSN)
	default:
		log.Fatalf("Unknown outputter: %s", *outputMode)
	}

	if outputter_err != nil {
		log.Fatalf("Couldn't create %s output: %+v", *outputMode, outputter_err)
	}

	err := outputter.CreateTiles()

	if err != nil {
		log.Fatalf("Failed to create %s output: %+v", *outputMode, err)
	}
	
	log.Printf("Created %s output\n", *outputMode)

	jobs := make(chan *TileRequest, 2000)
	results := make(chan *TileResponse, 2000)

	// Start up the HTTP workers that will fetch tiles
	workerWG := &sync.WaitGroup{}
	for w := 0; w < *numHTTPWorkers; w++ {
		workerWG.Add(1)
		go httpWorker(workerWG, w, httpClient, jobs, results)
	}

	// Start the worker that receives data from HTTP workers
	resultWG := &sync.WaitGroup{}
	resultWG.Add(1)
	go processResults(resultWG, results, outputter)

	consumer := func(tile *tilepack.Tile) {
		url := strings.NewReplacer(
			"{x}", fmt.Sprintf("%d", tile.X),
			"{y}", fmt.Sprintf("%d", tile.Y),
			"{z}", fmt.Sprintf("%d", tile.Z)).Replace(*urlTemplateStr)

		jobs <- &TileRequest{
			URL:  url,
			Tile: tile,
			Gzip: *gzipEnabled,
		}
	}

	opts := &tilepack.GenerateTilesOptions{
		Bounds:       bounds,
		Zooms:        zooms,
		ConsumerFunc: consumer,
		InvertedY:    *invertedY,
	}

	// Add tile request jobs
	tilepack.GenerateTiles(opts)
	close(jobs)
	log.Print("Job queue closed")

	// When the workers are done, close the results channel
	workerWG.Wait()
	close(results)
	log.Print("Finished making tile requests")

	// Wait for the results to be written out
	resultWG.Wait()
	log.Print("Finished processing tiles")
}
