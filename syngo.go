// syngo is a rsync like filesystem synchronization tool with the ability to
// keep a customizeable amount of back history
package main

import (
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("incorrect number of command line arguments\n\n")
		usage()
	}
	srcTree := strings.TrimSpace(os.Args[1])
	tgtTree := strings.TrimSpace(os.Args[2])
	if err := checkInput(srcTree, tgtTree); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("syncing %s to %s\n", srcTree, tgtTree)

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
