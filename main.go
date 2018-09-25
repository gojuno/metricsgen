package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gojuno/generator"
	"golang.org/x/tools/go/loader"
)

const version = "1.2"

type (
	programOptions struct {
		Interfaces []interfaceInfo
		Suffix     string
		OutputFile string
	}

	generateOptions struct {
		InterfaceName      string
		PackageName        string
		OutputFileName     string
		StructName         string
		SourcePackage      string
		DestinationPackage string
	}

	interfaceInfo struct {
		Package string
		Name    string
		Methods map[string]*types.Signature
	}

	visitor struct {
		*loader.Program
		interfaces      map[string]interfaceInfo
		sourceInterface string
	}
)

func main() {
	opts := processFlags()

	cfg := loader.Config{
		AllowErrors:         true,
		TypeCheckFuncBodies: func(string) bool { return false },
		TypeChecker: types.Config{
			IgnoreFuncBodies:         true,
			FakeImportC:              true,
			DisableUnusedImportCheck: true,
			Error: func(err error) {},
		},
	}

	for _, i := range opts.Interfaces {
		cfg.Import(i.Package)
	}

	outPackageRealPath := filepath.Dir(opts.OutputFile)
	stat, err := os.Stat(opts.OutputFile)
	if err != nil {
		if !os.IsNotExist(err) {
			die("failed to get file info for %s: %v", opts.OutputFile, err)
		}
	} else if stat.IsDir() {
		outPackageRealPath = opts.OutputFile
	}

	destImportPath, err := generator.PackageOf(outPackageRealPath)
	if err != nil {
		die("failed to detect import path of the %s: %v", outPackageRealPath, err)
	}

	cfg.Import(destImportPath)

	prog, err := cfg.Load()
	if err != nil {
		die("failed to load source code: %v", err)
	}

	packageName := prog.Package(destImportPath).Pkg.Name()

	for _, i := range opts.Interfaces {
		interfaces, err := findInterfaces(prog, i.Name, i.Package)
		if err != nil {
			die("%v", err)
		}

		for interfaceName, info := range interfaces {
			genOpts := generateOptions{
				SourcePackage:      i.Package,
				DestinationPackage: destImportPath,
				InterfaceName:      interfaceName,
				StructName:         interfaceName + "Metrics",
				OutputFileName:     filepath.Join(outPackageRealPath, сamelToSnake(interfaceName)+opts.Suffix),
				PackageName:        packageName,
			}

			if err := generate(prog, genOpts, info.Methods); err != nil {
				die("failed to generate %s: %v", genOpts.OutputFileName, err)
			}

			fmt.Printf("Generated file: %s\n", genOpts.OutputFileName)
		}
	}
}

func findInterfaces(prog *loader.Program, sourceInterface, sourcePackage string) (map[string]interfaceInfo, error) {
	v := &visitor{
		Program:         prog,
		sourceInterface: sourceInterface,
		interfaces:      make(map[string]interfaceInfo),
	}

	pkg := prog.Package(sourcePackage)
	if pkg == nil {
		return nil, fmt.Errorf("unable to load package: %s", sourcePackage)
	}

	for _, file := range pkg.Files {
		ast.Walk(v, file)
	}

	return v.interfaces, nil
}

func paramsToStructFields(p generator.ParamSet) string {
	var params []string
	for _, param := range p {
		params = append(params, fmt.Sprintf("%s %s", param.Name, param.Type))
	}

	return strings.Join(params, "\n")
}

func generate(prog *loader.Program, opts generateOptions, methods map[string]*types.Signature) error {
	gen := generator.New(prog)
	gen.ImportWithAlias(opts.DestinationPackage, "")
	gen.SetPackageName(opts.PackageName)
	gen.AddTemplateFunc("toStructFields", paramsToStructFields)
	gen.AddTemplateFunc("call", FuncCall(gen))
	gen.AddTemplateFunc("return", FuncReturn(gen))
	gen.SetVar("structName", opts.StructName)
	gen.SetVar("interfaceName", opts.InterfaceName)
	gen.SetVar("packagePath", opts.SourcePackage)
	gen.SetHeader(fmt.Sprintf(`DO NOT EDIT!
This code was generated automatically using github.com/gojuno/metricsgen v%s
The original interface %q can be found in %s`, version, opts.InterfaceName, opts.SourcePackage))
	gen.SetDefaultParamsPrefix("p")
	gen.SetDefaultResultsPrefix("r")

	if len(methods) == 0 {
		return fmt.Errorf("empty interface: %s", opts.InterfaceName)
	}

	if err := gen.ProcessTemplate("interface", template, methods); err != nil {
		return err
	}

	if err := os.Remove(opts.OutputFileName); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove output file %s: %v", opts.OutputFileName, err)
	}

	return gen.WriteToFilename(opts.OutputFileName)
}

// Visit implements ast.Visitor interface
func (v *visitor) Visit(node ast.Node) ast.Visitor {
	switch ts := node.(type) {
	case *ast.FuncDecl:
		return nil
	case *ast.TypeSpec:
		exprType, err := v.expressionType(ts.Type)
		if err != nil {
			die("failed to get expression for %T %s: %v", ts.Type, ts.Name.Name, err)
		}

		var i *types.Interface

		switch t := exprType.(type) {
		case *types.Named:
			underlying, ok := t.Underlying().(*types.Interface)
			if !ok {
				return nil
			}
			i = underlying
		case *types.Interface:
			i = t
		default:
			return nil
		}

		if ts.Name.Name == v.sourceInterface || v.sourceInterface == "*" {
			v.interfaces[ts.Name.Name] = interfaceInfo{
				Name:    ts.Name.Name,
				Methods: getInterfaceMethodsSignatures(i),
			}
		}

		return nil
	}

	return v
}

func (v *visitor) expressionType(e ast.Expr) (types.Type, error) {
	for _, info := range v.Program.AllPackages {
		if typesType := info.TypeOf(e); typesType != nil {
			return typesType, nil
		}
	}

	return nil, fmt.Errorf("expression not found: %+v", e)
}

func getInterfaceMethodsSignatures(t *types.Interface) map[string]*types.Signature {
	methods := make(map[string]*types.Signature)

	for i := 0; i < t.NumMethods(); i++ {
		methods[t.Method(i).Name()] = t.Method(i).Type().(*types.Signature)
	}

	return methods
}

const template = `
type {{$structName}} struct {
	next             {{$interfaceName}}
	summary          *prometheus.SummaryVec
	instanceName     string
}

func New{{$structName}}Summary(metricName string) *prometheus.SummaryVec {
	sv := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: metricName,
			Help: metricName,
		},
		[]string{"instance_name", "method"},
	)

	prometheus.MustRegister(sv)

	return sv
}

func New{{$structName}}WithSummary(next {{$interfaceName}}, instanceName string, sv *prometheus.SummaryVec) *{{$structName}} {
	return &{{$structName}} {
		next:     next,
		summary:  sv,
		instanceName:     instanceName,
	}
}

{{ range $methodName, $method := . }}
	func (m *{{$structName}}) {{$methodName}}{{signature $method}} {
		defer m.observe("{{$methodName}}", time.Now())

		{{ return $method }} m.next.{{$methodName}}({{call $method}})
	}
{{ end }}

func (m *{{$structName}}) observe(method string, startedAt time.Time) {
	duration := time.Since(startedAt)
	m.summary.WithLabelValues(m.instanceName, method).Observe(duration.Seconds())
}
`

func processFlags() *programOptions {
	var (
		help       = flag.Bool("h", false, "show this help message")
		interfaces = flag.String("i", "", "comma-separated names of the interfaces to mock, i.e fmt.Stringer,io.Reader, use io.* notation to generate metric decorators for all interfaces in an io package")
		output     = flag.String("o", "", "destination file name to place the generated mock or path to destination package when multiple interfaces are given")
		suffix     = flag.String("s", "_metrics.go", "output file name suffix which is added to file names when multiple interfaces are given")
		v          = flag.Bool("version", false, "show minimock version")
	)

	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if *v {
		fmt.Printf("minimock version: %s\n", version)
		os.Exit(0)
	}

	if *interfaces == "" {
		die("missing required parameter: -i, use -h flag for help")
	}

	if *output == "" {
		die("missing required parameter: -o, use -h flag for help")
	}

	interfacesList := []interfaceInfo{}
	for _, i := range strings.Split(*interfaces, ",") {
		chunks := strings.Split(i, ".")
		if len(chunks) < 2 {
			die("invalid interface name: %s\nname should be in the form <import path>.<interface type>, i.e. io.Reader\n", i)
		}

		importPath := getImportPath(strings.Join(chunks[0:len(chunks)-1], "."))

		interfacesList = append(interfacesList, interfaceInfo{Package: importPath, Name: chunks[len(chunks)-1]})
	}

	return &programOptions{
		Interfaces: interfacesList,
		OutputFile: *output,
		Suffix:     *suffix,
	}
}

func getImportPath(realPath string) string {
	_, err := os.Stat(realPath)
	if err == nil {
		importPath, err := generator.PackageOf(realPath)
		if err != nil {
			die("failed to detect import path of the %s: %v", realPath, err)
		}

		return importPath
	}

	return realPath
}

//FuncCall returns a signature of the function represented by f
//f can be one of: ast.Expr, ast.SelectorExpr, types.Type, types.Signature
func FuncCall(g *generator.Generator) interface{} {
	return func(f interface{}) (string, error) {
		params, err := g.FuncParams(f)
		if err != nil {
			return "", fmt.Errorf("failed to get %+v func params: %v", f, err)
		}

		names := []string{}
		for _, param := range params {
			names = append(names, param.Pass())
		}

		return strings.Join(names, ", "), nil
	}
}

func FuncReturn(g *generator.Generator) interface{} {
	return func(f interface{}) (string, error) {
		params, err := g.FuncResults(f)
		if err != nil {
			return "", fmt.Errorf("failed to get %+v func results: %v", f, err)
		}

		if len(params) == 0 {
			return "", nil
		}

		return "return ", nil
	}
}

type buffer struct {
	r         []byte
	runeBytes [utf8.UTFMax]byte
}

func (b *buffer) write(r rune) {
	if r < utf8.RuneSelf {
		b.r = append(b.r, byte(r))
		return
	}
	n := utf8.EncodeRune(b.runeBytes[0:], r)
	b.r = append(b.r, b.runeBytes[0:n]...)
}

func (b *buffer) indent() {
	if len(b.r) > 0 {
		b.r = append(b.r, '_')
	}
}

// CamelToSnake transforms strings from CamelCase to snake_case
func сamelToSnake(s string) string {
	b := buffer{
		r: make([]byte, 0, len(s)),
	}
	var m rune
	var w bool
	for _, ch := range s {
		if unicode.IsUpper(ch) {
			if m != 0 {
				if !w {
					b.indent()
					w = true
				}
				b.write(m)
			}
			m = unicode.ToLower(ch)
		} else {
			if m != 0 {
				b.indent()
				b.write(m)
				m = 0
				w = false
			}
			b.write(ch)
		}
	}
	if m != 0 {
		if !w {
			b.indent()
		}
		b.write(m)
	}
	return string(b.r)
}

func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "metricsgen: "+format+"\n", args...)
	os.Exit(1)
}
