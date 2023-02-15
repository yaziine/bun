package mssqldialect

import (
	"reflect"

	"github.com/yaziine/bun/schema"
)

func scanner(typ reflect.Type) schema.ScannerFunc {
	return schema.Scanner(typ)
}
