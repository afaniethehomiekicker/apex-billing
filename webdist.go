package main

import "embed"

// The built web pages (frontend/dist), compiled into the program.
//
//go:embed all:frontend/dist
var frontendDist embed.FS
