// NOTE: Generated By hrp v4.3.3, DO NOT EDIT!
package main

import (
	"github.com/httprunner/funplugin/fungo"
)

func main() {
	fungo.Register("SumTwoInt", SumTwoInt)
	fungo.Register("SumInts", SumInts)
	fungo.Register("Sum", Sum)
	fungo.Register("SetupHookExample", SetupHookExample)
	fungo.Register("TeardownHookExample", TeardownHookExample)
	fungo.Register("GetUserAgent", GetUserAgent)
	fungo.Serve()
}
