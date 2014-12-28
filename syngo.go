// syngo is a rsync like filesystem synchronization tool with the ability to
// keep a customizeable amount of back history
package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"
)

// hardcoded number of concurrent goroutine tasks (for now)
const numCheckers = 3
const numSyncers = 2

// syncStats keeps a record of useful sync statistics (number of files,
// amount of data, ...)
type syncStats struct {
	numFiles int64
	numBytes int64
}

// fileInfo keeps track of the information needed to determine if a file needs
// to be resynced or not
type fileInfo struct {
	info     os.FileInfo
	path     string
	linkPath string // target path for symbolic links
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	if len(os.Args) != 3 {
		fmt.Printf("incorrect number of command line arguments\n\n")
		usage()
	}

	startTime := time.Now()

	srcTree := path.Clean(strings.TrimSpace(os.Args[1]))
	tgtTree := path.Clean(strings.TrimSpace(os.Args[2]))
	if err := checkInput(srcTree, tgtTree); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("syncing %s to %s\n", srcTree, tgtTree)

	// synchronize directory layout between source and target
	dirList := make(chan fileInfo)
	go parseSrcDirs(srcTree, dirList)

	var dirSync sync.WaitGroup
	dirSync.Add(numCheckers)
	for i := 0; i < numCheckers; i++ {
		go syncDirLayout(tgtTree, dirList, &dirSync)
	}
	dirSync.Wait()

	// synchronize files between source and target
	fileList := make(chan fileInfo)
	go parseSrcFiles(srcTree, fileList)

	updateList := make(chan fileInfo)
	var done sync.WaitGroup
	done.Add(numCheckers)
	for i := 0; i < numCheckers; i++ {
		go checkTgt(tgtTree, fileList, updateList, &done)
	}
	go chanCloser(updateList, &done)

	//var syncDone sync.WaitGroup
	//syncDone.Add(numSyncers)
	syncDone := make(chan syncStats)
	for i := 0; i < numSyncers; i++ {
		go syncFiles(srcTree, tgtTree, updateList, syncDone)
	}

	var numFiles, numBytes int64
	for i := 0; i < numSyncers; i++ {
		d := <-syncDone
		numFiles += d.numFiles
		numBytes += d.numBytes
	}
	numMBytes := float64(numBytes) / 1024 / 1024
	dur := time.Since(startTime).Seconds()
	fmt.Printf("Synced %d files with %.5g MB in %.5g s (%.5g MB/s)\n", numFiles,
		numMBytes, dur, numMBytes/dur)
	fmt.Println("done syncing")

}

// usage provides a simple usage string
func usage() {
	fmt.Println("usage: syngo <source tree> <target tree>")
	os.Exit(1)
}

// checkInput does some basic sanity check on the provided input
// NOTE: This check only makes sense if src and dst are local file trees. In
// the future this will need to be changed and made more robust.
func checkInput(src, dst string) error {
	if src == dst {
		return fmt.Errorf("source and target tree cannot be identical")
	}

	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a valid source directory tree", src)
	}

	return nil
}
