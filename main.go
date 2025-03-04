package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {
	f, err := os.Create(".profile/cpu-profile.prof")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		panic(err)
	}
	defer pprof.StopCPUProfile()

	start := time.Now()
	doStuff()
	fmt.Printf("Time it took since start: %v seconds\n", time.Now().Sub(start))
}

type stationData struct {
	min float32
	max float32
	sum float32
	cnt int
}

func doStuff() {
	f, err := os.Open("measurements.txt")
	if err != nil {
		log.Fatalf("Error while opening the file, %v", err)
	}
	defer f.Close()

	cpus := runtime.NumCPU()
	bytesStream := make(chan []byte, cpus)

	stream := make(chan map[string]stationData, cpus)

	var wg sync.WaitGroup

	go func() {
		chunkSize := 4 * 1024 * 1024
		readChunk := make([]byte, chunkSize)
		// content of leftover last line which doesn't end in '\n' delimiter
		// this belongs to newline, but because of buffer size we were unable to accomondate whole line.
		leftChunk := make([]byte, 0, chunkSize)

		for {
			read, err := f.Read(readChunk)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				log.Fatalf("Error in reading from file\n error is: %v", err)
			}
			readChunk = readChunk[:read]
			sendUpto := bytes.LastIndex(readChunk, []byte{'\n'})

			// sending slice to channel and writing again on same slice can cause deadlocks
			// hence always make a copy of the slice for sending purpose
			sendCopy := append(leftChunk, readChunk[:sendUpto]...)
			leftChunk = make([]byte, len(readChunk[sendUpto+1:]))
			leftChunk = append(leftChunk, readChunk[sendUpto+1:]...)

			bytesStream <- sendCopy
		}
		close(bytesStream)
	}()

	for cpu := 0; cpu < cpus; cpu++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for byteChunk := range bytesStream {
				processChunk(byteChunk, stream)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(stream)
	}()

	mp := make(map[string]stationData)
	for smallMap := range stream {
		for city, info := range smallMap {
			if station, ok := mp[city]; ok {
				station.sum += info.sum
				station.cnt += info.cnt

				if info.max > station.max {
					station.max = info.max
				}
				if info.min < station.min {
					station.min = info.min
				}
			} else {
				mp[city] = info
			}
		}
	}
	printStuff(mp)
}

func processChunk(buffer []byte, stream chan<- map[string]stationData) {
	var builder strings.Builder
	mp := make(map[string]stationData)

	var city string
	for _, ch := range buffer {
		if ch == '\n' {
			if builder.Len() != 0 {
				tempStr := builder.String()
				builder.Reset()

				temp64, err := strconv.ParseFloat(tempStr, 64)
				if err != nil {
					log.Fatalf("failed to convert %s into float", tempStr)
				}
				temp := float32(temp64)

				station, ok := mp[city]
				if !ok {
					mp[city] = stationData{temp, temp, temp, 1}
				} else {
					if temp < station.min {
						station.min = temp
					} else if temp > station.max {
						station.max = temp
					}
					station.sum += temp
					station.cnt++
				}
			}
		} else if ch == ';' {
			city = builder.String()
			builder.Reset()
		} else {
			builder.WriteByte(ch)
		}
	}
	stream <- mp
}

func printStuff(mp map[string]stationData) {
	cities := make([]string, len(mp))

	for key := range mp {
		cities = append(cities, key)
	}

	sort.Strings(cities)

	print("{\n")
	for _, city := range cities {
		val := mp[city]
		fmt.Printf("%s=%0.1f/%0.1f/%0.1f\n", city, val.min, val.sum/float32(val.cnt), val.max)
	}
	print("}\n")
}
