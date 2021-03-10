package main

import (
	"hash/crc32"
	"io/ioutil"
	"log"
)

func main() {
	b, err := ioutil.ReadFile("x86_64-repodata")
	if err != nil {
		log.Println(err)
		return
	}
	log.Println(crc32.ChecksumIEEE(b))
}
