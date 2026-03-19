module github.com/Avalanche-io/c4sh

go 1.16

replace github.com/Avalanche-io/c4 => ../c4

require (
	github.com/Avalanche-io/c4 v1.0.0
	github.com/mattn/go-isatty v0.0.20
)

require golang.org/x/sys v0.42.0 // indirect
