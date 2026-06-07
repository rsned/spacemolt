// Command struct-audit dumps Go struct definitions (name, fields, json tags,
// resolved types) as JSON for the import-struct audit pipeline.
//
// It is the Go half of the audit; the comparison + report generation lives in
// audit.py (which invokes this command). Run via audit.py, or directly:
//
//	go run ./cmd/data/struct-audit pkg/game/serverapi
//
// Output: a JSON array of {name, file, fields:[{json,go_name,go_type,omitempty,inline}]}
// on stdout. Only stdlib is used so it compiles standalone within the module.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type Field struct {
	JSON      string `json:"json"`
	GoName    string `json:"go_name"`
	GoType    string `json:"go_type"`
	Omitempty bool   `json:"omitempty"`
	Inline    bool   `json:"inline"`
}

type Struct struct {
	Name   string  `json:"name"`
	File   string  `json:"file"`
	Fields []Field `json:"fields"`
}

func typeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.ArrayType:
		return "[]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{...}"
	default:
		return fmt.Sprintf("%T", e)
	}
}

func main() {
	dirs := os.Args[1:]
	var out []Struct
	fset := token.NewFileSet()
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, fpath := range files {
			if strings.HasSuffix(fpath, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, fpath, nil, 0)
			if err != nil {
				continue
			}
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					s := Struct{Name: ts.Name.Name, File: filepath.Base(fpath)}
					for _, fld := range st.Fields.List {
						gt := typeString(fld.Type)
						jsonName, omit, inline := "", false, false
						if fld.Tag != nil {
							tag := strings.Trim(fld.Tag.Value, "`")
							if idx := strings.Index(tag, `json:"`); idx >= 0 {
								rest := tag[idx+6:]
								end := strings.Index(rest, `"`)
								val := rest[:end]
								parts := strings.Split(val, ",")
								jsonName = parts[0]
								for _, p := range parts[1:] {
									if p == "omitempty" {
										omit = true
									}
									if p == "inline" {
										inline = true
									}
								}
							}
						}
						if len(fld.Names) == 0 {
							s.Fields = append(s.Fields, Field{JSON: jsonName, GoName: gt, GoType: gt, Omitempty: omit, Inline: true})
							continue
						}
						for _, n := range fld.Names {
							if !n.IsExported() {
								continue
							}
							jn := jsonName
							if jn == "" {
								jn = n.Name
							}
							s.Fields = append(s.Fields, Field{JSON: jn, GoName: n.Name, GoType: gt, Omitempty: omit, Inline: inline})
						}
					}
					out = append(out, s)
				}
			}
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	_ = enc.Encode(out)
}
