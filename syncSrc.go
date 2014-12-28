// syncSrc contains functions related to syncing content in the source location
package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// parseSrcDirs determines the directory layout of the src tree.
// NOTE: use of filepath.Walk is inefficient for large numbers of files and
// should be replaced eventually
func parseSrcDirs(src string, dirList chan<- fileInfo) {
	filepath.Walk(src, func(p string, i os.FileInfo, err error) error {
		if err != nil {
			log.Print(err)
			return nil
		}

		relPath := filepath.Clean(strings.TrimPrefix(p, src))
		if i.IsDir() {
			dirList <- fileInfo{info: i, path: relPath}
		}
		return nil
	})
	close(dirList)
}

// parseSrcFiles determined the files that need to be checked for syncing based on
// the provided src location. For now, this simply performs a file system
// walk starting at src.
// NOTE: use of filepath.Walk is inefficient for large numbers of files and
// should be replaced eventually
func parseSrcFiles(src string, fileList chan<- fileInfo) {
	filepath.Walk(src, func(p string, i os.FileInfo, err error) error {
		if err != nil {
			log.Print(err)
			return nil
		}

		if i.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(src+"/", p)
		if err != nil {
			log.Printf("in parseSrcFiles: %s\n", err)
			return nil
		}

		// deal with symbolic links
		var symPath string
		if i.Mode()&os.ModeSymlink != 0 {
			symPath, err = os.Readlink(p)
			if err != nil {
				log.Printf("++++ in parseSrcFiles: %s\n", err)
				return nil
			}
		}

		fileList <- fileInfo{info: i, path: relPath, linkPath: symPath}
		return nil
	})
	close(fileList)
}
