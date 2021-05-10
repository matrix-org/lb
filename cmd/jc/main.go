package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/matrix-org/lb"
)

var (
	flagCBORToJSON = flag.Bool("c2j", false, "CBOR -> JSON")
	flagVer        = flag.String("v", "1", "CBOR integer key version, currently only '1' is supported.")
	flagOutput     = flag.String("out", "-", "Output file to write to. If '-' prints to stdout")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of jc:\n")
		flag.PrintDefaults()
		fmt.Println("\nMust supply either a file '@some-file', stdin '-', or the raw data '{}'")
		fmt.Println(`Example JSON->CBOR literal to file:                ./jc -out "output.cbor" '{"hello":"world"}'`)
		fmt.Println(`Example JSON->CBOR file to file:                   ./jc -out "output.cbor" '@data.json'`)
		fmt.Println(`Example JSON->CBOR stdin:         echo '[42,38]' | ./jc -out "output.cbor" -`)
		fmt.Println(`Example CBOR->JSON file to file:                   ./jc -c2j -out "output.json" '@output.cbor'`)
		fmt.Println(`Example CBOR->JSON file to stdout:                 ./jc -c2j '@output.cbor'`)
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	if *flagVer != "1" {
		log.Printf("FATAL: Only version '1' is supported.")
		os.Exit(1)
	}

	inputFlag := flag.Arg(0)
	var reqBody io.Reader
	if inputFlag == "-" {
		reqBody = os.Stdin
	} else if strings.HasPrefix(inputFlag, "@") {
		f, err := os.Open(inputFlag[1:])
		if err != nil {
			log.Printf("FATAL reading request file: %s\n", err.Error())
			os.Exit(1)
		}
		reqBody = f
		defer f.Close()
	} else {
		reqBody = bytes.NewBufferString(inputFlag)
	}

	var output []byte
	var err error

	codec := lb.NewCBORCodecV1(true)

	if *flagCBORToJSON {
		output, err = codec.CBORToJSON(reqBody)
	} else {
		output, err = codec.JSONToCBOR(reqBody)
	}

	if err != nil {
		log.Printf("FATAL: %s", err)
		os.Exit(1)
	}
	if *flagOutput == "-" {
		fmt.Printf(string(output))
	} else {
		ioutil.WriteFile(*flagOutput, output, os.ModePerm)
		fmt.Printf("Output to '%s' (%d bytes) %x\n", *flagOutput, len(output), output)
	}
}
