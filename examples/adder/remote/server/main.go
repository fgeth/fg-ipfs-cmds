package main

import (
	nethttp "net/http"

	"github.com/fgeth/fg-ipfs-cmds/examples/adder"

	http "github.com/fgeth/fg-ipfs-cmds/http"
)

type env struct{}

func main() {
	h := http.NewHandler(env{}, adder.RootCmd, http.NewServerConfig())

	// create http rpc server
	err := nethttp.ListenAndServe(":6798", h)
	if err != nil {
		panic(err)
	}
}
