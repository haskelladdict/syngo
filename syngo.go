// syngo is a rsync like filesystem synchronization tool with the ability to
// keep a customizeable amount of back history
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
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

// chanCloser closes the provided fileInfo channel once the provided done channel
// has delievered the specified number of elements
func chanCloser(fileList chan<- fileInfo, done *sync.WaitGroup) {
	done.Wait()
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
