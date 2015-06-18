package generator

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"

	_ "golang.org/x/tools/go/gcimporter"
	"golang.org/x/tools/go/types"
)

const BaseDocument = "github.com/tyba/storable.Document"

type Processor struct {
	Path   string
	Ignore map[string]bool

	fieldsForStr map[*types.Struct]*[]*Field
}

func NewProcessor(path string, ignore []string) *Processor {
	i := make(map[string]bool, 0)
	for _, file := range ignore {
		i[file] = true
	}

	return &Processor{
		Path:         path,
		Ignore:       i,
		fieldsForStr: map[*types.Struct]*[]*Field{},
	}
}

func (p *Processor) Do() (*Package, error) {
	files, err := p.getSourceFiles()
	if err != nil {
		return nil, err
	}

	typesPkg, _ := p.parseSourceFiles(files)
	return p.ProcessTypesPkg(typesPkg)
}

func (p *Processor) ProcessTypesPkg(typesPkg *types.Package) (*Package, error) {
	pkg := &Package{Name: typesPkg.Name()}
	p.processPackage(pkg, typesPkg)
	fmt.Println("processed")

	return pkg, nil
}

func (p *Processor) getSourceFiles() ([]string, error) {
	pkg, err := build.Default.ImportDir(p.Path, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot process directory %s: %s", p.Path, err)
	}

	var files []string
	files = append(files, pkg.GoFiles...)
	files = append(files, pkg.CgoFiles...)

	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no buildable Go files", p.Path)
	}

	return joinDirectory(p.Path, files), nil
}

func (p *Processor) parseSourceFiles(filenames []string) (*types.Package, error) {
	var files []*ast.File
	fs := token.NewFileSet()
	for _, filename := range filenames {
		if _, ok := p.Ignore[filename]; ok {
			continue
		}

		file, err := parser.ParseFile(fs, filename, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing package: %s: %s", filename, err)
		}

		files = append(files, file)
	}

	config := types.Config{FakeImportC: true, Error: func(error) {}}
	info := &types.Info{}

	return config.Check(p.Path, fs, files, info)
}

func (p *Processor) processPackage(pkg *Package, typesPkg *types.Package) {
	pkg.Models = make([]*Model, 0)
	pkg.Structs = make([]string, 0)
	pkg.Functions = make([]string, 0)

	s := typesPkg.Scope()
	for _, name := range s.Names() {
		fun := p.tryGetFunction(s.Lookup(name))
		if fun != nil {
			pkg.Functions = append(pkg.Functions, name)
		}

		str := p.tryGetStruct(s.Lookup(name).Type())
		if str == nil {
			continue
		}

		if m := p.processStruct(name, str); m != nil {
			pkg.Models = append(pkg.Models, m)
		} else {
			pkg.Structs = append(pkg.Structs, name)
		}
	}
}

func (p *Processor) tryGetFunction(typ types.Object) *types.Func {
	switch t := typ.(type) {
	case *types.Func:
		return t
	}

	return nil
}

func (p *Processor) tryGetStruct(typ types.Type) *types.Struct {
	switch t := typ.(type) {
	case *types.Named:
		return p.tryGetStruct(t.Underlying())
	case *types.Pointer:
		return p.tryGetStruct(t.Elem())
	case *types.Slice:
		return p.tryGetStruct(t.Elem())
	case *types.Map:
		return p.tryGetStruct(t.Elem())
	case *types.Struct:
		return t
	}

	return nil
}

func (p *Processor) processStruct(name string, s *types.Struct) *Model {
	m := NewModel(name)

	var base int
	if base, m.Fields = p.getFields(s); base == -1 {
		return nil
	}

	p.procesBaseField(m, m.Fields[base])
	return m
}

func (p *Processor) getFields(s *types.Struct) (base int, fields []*Field) {
	base = p.getFields2(s)

	for _, fields := range p.fieldsForStr {
		for _, f := range *fields {
			if f.CheckedNode == nil {
				continue
			}
			if len(f.Fields) == 0 {
				f.SetFields(*p.fieldsForStr[p.tryGetStruct(f.CheckedNode.Type())])
			}
		}
	}

	fields = *p.fieldsForStr[s]

	return
}

func (p *Processor) getFields2(s *types.Struct) int {
	c := s.NumFields()

	base := -1
	fields := make([]*Field, 0)
	if _, ok := p.fieldsForStr[s]; !ok {
		p.fieldsForStr[s] = &fields
	}

	for i := 0; i < c; i++ {
		f := s.Field(i)
		if !f.Exported() {
			continue
		}

		t := reflect.StructTag(s.Tag(i))
		if f.Type().String() == BaseDocument {
			base = i
		}

		field := NewField(f.Name(), f.Type().Underlying().String(), t)
		str := p.tryGetStruct(f.Type())
		if f.Type().String() != BaseDocument && str != nil {
			field.Type = getStructType(f.Type())
			field.CheckedNode = f
			_, ok := p.fieldsForStr[str]
			if !ok {
				p.getFields2(str)
			}
		}

		fields = append(fields, field)
	}

	return base
}

func getStructType(t types.Type) string {
	ts := t.String()
	if ts != "time.Time" && ts != "bson.ObjectId" {
		return "struct"
	}

	return ts
}

func (p *Processor) procesBaseField(m *Model, f *Field) {
	m.Collection = f.Tag.Get("collection")
}

func joinDirectory(directory string, files []string) []string {
	r := make([]string, len(files))
	for i, file := range files {
		r[i] = filepath.Join(directory, file)
	}

	return r
}
