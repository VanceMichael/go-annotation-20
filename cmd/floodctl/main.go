// Command floodctl 是防汛水情监测与溃口抢险调度平台的命令行入口。
package main

import (
	"os"

	"floodwatch/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
