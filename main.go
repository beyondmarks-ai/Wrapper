package main

import (
	"embed"

	"github.com/beyondmarks-ai/Wrapper/src/cmd"
)

var (
	//go:embed src/wrapper_config/*
	content embed.FS
)

func main() {
	cmd.Run(content)
}
