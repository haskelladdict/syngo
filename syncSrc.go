// syncSrc contains functions related to syncing content in the source location
package main

import (
	"log"
	"os"
	"path"
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

		relPath := strings.TrimPrefix(p, src)
		if i.IsDir() {
			dirList <- fileInfo{info: i, path: relPath}
		}
		return nil
	})
	close(dirList)
}

// parseSrcFiles determined the files that need to be checked for syncing based on
// the provided src destinations. For now, this simply performs a fime system
// walk starting at src.
// NOTE: use of filepath.Walk is inefficient for large numbers of files and
// should be replaced eventually
func parseSrcFiles(src string, fileList chan<- fileInfo) {
	filepath.Walk(src, func(p string, i os.FileInfo, err error) error {
		if err != nil {
			log.Print(err)
			return nil
		}

		relPath := strings.TrimPrefix(p, src+"/")
		if !i.IsDir() {
			var relSymPath string
			if i.Mode()&os.ModeSymlink != 0 {
				symp, err := filepath.EvalSymlinks(p)
				if err != nil {
					return nil
				}
				// if symlink is absolute path we leave it unchanged otherwise adjust
				// target path
				if filepath.IsAbs(symp) {
					relSymPath = symp
				} else {
					relSymPath = strings.TrimPrefix(symp, path.Dir(p)+"/")
				}
			}
			fileList <- fileInfo{info: i, path: relPath, linkPath: relSymPath}
		}
		return nil
	})
	close(fileList)
}
