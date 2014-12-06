// syngo is a rsync like filesystem synchronization tool with the ability to
// keep a customizeable amount of back history
package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("incorrect number of command line arguments\n\n")
		usage()
	}
	srcTree := path.Clean(strings.TrimSpace(os.Args[1]))
	tgtTree := path.Clean(strings.TrimSpace(os.Args[2]))
	if err := checkInput(srcTree, tgtTree); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("syncing %s to %s\n", srcTree, tgtTree)

	// fileList is a channel with a list of files (as fileInfo types)
	// to be assessed for syncing
	fileList := make(chan fileInfo)
	go parseSrc(srcTree, fileList)

	// updateList is a channel with a list of files (as fileInfo types) that need
	// to be synced
	updateList := make(chan fileInfo)
	go checkTgt(tgtTree, fileList, updateList)

	// updateInfo is a channel with files
	for _ = range updateList {
		//fmt.Println(i.path, i.info.Size(), i.info.ModTime())
	}
}

// fileInfo keeps track of the information needed to determine if a file needs
// to be resynced or not
type fileInfo struct {
	info os.FileInfo
	path string
}

// checkTgt processes a channel of target fileInfo types and determines if
// entry needs to be synced or not. Directories will be greated if they
// don't exist
// XXX: I am assuming here the os.MkdirAll is threadsafe which it most likely
// isn't. Thus, this steps needs more thought going forward.
func checkTgt(tgt string, fileList <-chan fileInfo, updateList chan<- fileInfo) {
	for f := range fileList {
		path := filepath.Join(tgt, f.path)
		if f.info.IsDir() {
			_, err := os.Lstat(path)
			if err != nil && os.IsNotExist(err) {
				//fmt.Println(path, "does not exist")
				err := os.MkdirAll(path, f.info.Mode())
				if err != nil {
					log.Print(err)
				}
			}
		}
	}
	close(updateList)
}

// parseSrc determined the files that need to be checked for syncing based on
// the provided src destinations. For now, this simply performs a fime system
// walk starting at src.
// NOTE: use of filepath.Walk is inefficient for large numbers of files and
// should be replaced eventually
func parseSrc(src string, fileList chan<- fileInfo) {
	filepath.Walk(src, func(p string, i os.FileInfo, err error) error {
		if err != nil {
			log.Print(err)
		} else {
			relPath := strings.TrimPrefix(p, src)
			fileList <- fileInfo{info: i, path: relPath}
		}
		return nil
	})
	close(fileList)
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
