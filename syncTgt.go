// syncSrc contains functions related to syncing content in the source location
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// syncFiles processes a list of files which need to be synced and processes
// them one by one
// NOTE: Currently we only deal with regular files and symlinks, all others are
// skipped
func syncFiles(src, tgt string, fileList <-chan fileInfo, syncDone chan<- syncStats) {
	var numBytes int64
	var fileCount int64
	for file := range fileList {
		srcPath := filepath.Join(src, file.path)
		tgtPath := filepath.Join(tgt, file.path)

		fileMode := file.info.Mode()
		if fileMode.IsRegular() {
			n, err := syncFile(srcPath, tgtPath, file)
			if err != nil {
				log.Print(err)
				continue
			}
			numBytes += n

		} else if fileMode&os.ModeSymlink != 0 {
			if _, err := os.Lstat(tgtPath); err == nil {
				if err := os.Remove(tgtPath); err != nil {
					log.Printf("failed to remove stale symbolic link %s: %s\n", tgtPath, err)
					continue
				}
			}

			linkPath := file.linkPath
			if err := os.Symlink(linkPath, tgtPath); err != nil {
				log.Printf("failed to create symbolic link %s to %s: %s\n", tgtPath,
					linkPath, err)
				continue
			}

		} else {
			continue
		}
		fileCount++
	}
	syncDone <- syncStats{numFiles: fileCount, numBytes: numBytes}
}

// syncDirLayout syncs the target directory layout with the provided source layout.
// XXX: This function assumes that os.MkdirAll is threadsafe which it most
// likely isn't. Thus, this steps needs much more thought going forward.
func syncDirLayout(tgt string, dirList <-chan fileInfo, done *sync.WaitGroup) {
	for dir := range dirList {
		tgtPath := filepath.Join(tgt, dir.path)
		_, err := os.Lstat(tgtPath)
		if err != nil && os.IsNotExist(err) {
			err := os.MkdirAll(tgtPath, dir.info.Mode())
			if err != nil {
				log.Print(err)
			}
		}
	}
	done.Done()
}

// checkTgt processes a channel of target fileInfo types and determines if
// entry needs to be synced or not.
func checkTgt(tgt string, fileList <-chan fileInfo, updateList chan<- fileInfo,
	done *sync.WaitGroup) {
	for srcFile := range fileList {

		path := filepath.Join(tgt, srcFile.path)
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				updateList <- srcFile
			} else {
				log.Print(err)
			}
			continue
		}

		if srcFile.info.Mode()&os.ModeSymlink != 0 {
			if info.Mode()&os.ModeSymlink != 0 {
				// check that link points to the correct file
				symp, err := filepath.EvalSymlinks(path)
				if err != nil {
					continue
				}
				// adjust the sym link path for relative paths
				relSymPath := symp
				if !filepath.IsAbs(symp) {
					relSymPath = strings.TrimPrefix(symp, filepath.Dir(path)+"/")
				}
				if relSymPath != srcFile.linkPath {
					updateList <- srcFile
				}
			} else {
				updateList <- srcFile
			}
		} else {
			if (srcFile.info.Size() != info.Size()) ||
				(srcFile.info.Mode() != info.Mode()) ||
				(srcFile.info.ModTime() != info.ModTime()) {
				updateList <- srcFile
			}
		}
	}
	done.Done()
}

// chanCloser closes the provided fileInfo channel once the provided done channel
// has delievered the specified number of elements
func chanCloser(fileList chan<- fileInfo, done *sync.WaitGroup) {
	done.Wait()
	close(fileList)
}

// syncFile synchronizes target and source and makes sure they have identical
// permissions and timestamps
func syncFile(srcPath, tgtPath string, file fileInfo) (int64, error) {
	s, err := os.Open(srcPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open file %s for syncing: %s\n", srcPath, err)
	}

	t, err := os.Create(tgtPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create file %s for syncing: %s\n", tgtPath, err)
	}

	n, err := io.Copy(t, s)
	if err != nil {
		log.Printf("failed to copy file %s to %s during syncing: %s\n", srcPath,
			tgtPath, err)
	}

	// sync file properties between source and target
	if err := os.Chtimes(tgtPath, file.info.ModTime(), file.info.ModTime()); err != nil {
		log.Printf("failed to change file modification time for %s: %s\n", tgtPath, err)
	}

	if err := os.Chmod(tgtPath, file.info.Mode()); err != nil {
		log.Printf("failed to change file mode for %s: %s\n", tgtPath, err)
	}

	return n, nil
}
