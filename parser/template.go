package parser

import (
	"bytes"
	"embed"
	"path/filepath"
	"strings"
	"text/template"

	log "github.com/ml444/glog"

	"github.com/ml444/gctl/util"
)

//func ParseTemplateToFile(pd *ProtoData, basePath, tempDir, tempName string, funcMap template.FuncMap) error {
//	fPath := filepath.Join(basePath, pd.Options["go_package"], strings.TrimSuffix(tempName, viper.GetString("template_format_suffix")))
//	return GenerateTemplate(fPath, tempDir, tempName, pd, funcMap)
//}

var funcMap = template.FuncMap{
	"Concat":                   util.Concat,
	"TrimSpace":                strings.TrimSpace,
	"TrimPrefix":               strings.TrimPrefix,
	"HasPrefix":                strings.HasPrefix,
	"Contains":                 strings.Contains,
	"ToUpper":                  strings.ToUpper,
	"ToUpperFirst":             util.ToUpperFirst,
	"ToLowerFirst":             util.ToLowerFirst,
	"ToSnakeCase":              util.ToSnakeCase,
	"ToCamelCase":              util.ToCamelCase,
	"Add":                      util.Add,
	"GetStatusCodeFromComment": util.GetStatusCodeFromComment,
}

func GenerateTemplate(fPath string, tfs embed.FS, tempFile string, data interface{}) error {
	var err error
	f, err := util.OpenFile(fPath)
	if err != nil {
		return err
	}
	_, tempName := filepath.Split(tempFile)
	temp := template.New(tempName)
	if funcMap != nil {
		temp.Funcs(funcMap)
	}
	temp, err = temp.ParseFS(tfs, tempFile)
	//temp, err = temp.ParseFiles(tempFile)
	if err != nil {
		log.Error(err)
		return err
	}
	err = temp.Execute(f, data)
	if err != nil {
		log.Printf("Can't generate file %s,Error :%v\n", fPath, err)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

var svcMethodTemp = `
{{$pn := .PackageName}}

{{ range $i, $svc := .ServiceList }}
{{$sn := ToCamelCase $svc.ServiceName}}
{{$svcName := Concat $sn "Service" }}

{{ range $j, $v := $svc.RpcList }}
func (s {{ $svcName }}) {{$v.Name}}(ctx context.Context, req *{{$pn}}.{{$v.RequestType}}) (*{{$pn}}.{{$v.ResponseType}}, error) {
	var rsp {{$pn}}.{{$v.ResponseType}}
	return &rsp, nil
}
{{ end }}
{{ end }}
`

func GenerateServiceMethodContent(pd *ParseData) ([]byte, error) {

	temp := template.New("svcMethodTemp")
	if funcMap != nil {
		temp.Funcs(funcMap)
	}
	temp, err := temp.Parse(svcMethodTemp)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	var buffer bytes.Buffer
	err = temp.Execute(&buffer, pd)
	if err != nil {
		log.Printf("Can't generate file content,Error :%v\n", err)
		return nil, err
	}
	return buffer.Bytes(), nil
}

var daoTemp = `
{{$pn := .PackageName}}
func init() {
	db.RegisterModel(
{{- range $i, $m := .ModelList -}}
		&{{$pn}}.{{$m.Name}}{},
{{- end -}}
	)
}

{{ range $i, $m := .ModelList }}
    {{$TModelName := TrimPrefix $m.Name "Model" }}
    {{$cliModelName := Concat $pn "." $m.Name}}
var db{{$TModelName}} = NewT{{$TModelName}}(db.DB())

type T{{$TModelName}} struct {
	db    *gorm.DB
	model *{{$cliModelName}}
}

func NewT{{$TModelName}}(db *gorm.DB) *T{{$TModelName}} {
	return &T{{$TModelName}}{
		db:    db,
		model: &{{$cliModelName}}{},
	}
}

func (d *T{{$TModelName}}) newScope() *dbx.Scope {
    if d.db == nil {
		d.db = db.DB()
	}
	return dbx.NewScope(d.db, &{{$cliModelName}}{})
}

func (d *T{{$TModelName}}) Create(ctx context.Context, m *{{$cliModelName}}) error {
	return d.newScope().Create(&m)
}

func (d *T{{$TModelName}}) Update(ctx context.Context, m interface{}, whereMap map[string]interface{}) error {
	return d.newScope().Where(whereMap).Update(&m)
}

func (d *T{{$TModelName}}) DeleteById(ctx context.Context, pk uint64) error {
	return d.newScope().Eq(dbId, pk).Delete()
}

func (d *T{{$TModelName}}) DeleteByWhere(ctx context.Context, whereMap map[string]interface{}) error {
	return d.newScope().Where(whereMap).Delete()
}

func (d *T{{$TModelName}}) GetOne(ctx context.Context, pk uint64) (*{{$cliModelName}}, error) {
	var m {{$cliModelName}}
	err := d.newScope().SetNotFoundErr({{$pn}}.ErrNotFound{{$TModelName}}).First(&m, pk)
	return &m, err
}
{{$sModelName := ToLowerFirst $TModelName}}
{{$reqType := Concat "List" $TModelName "Req"}}
func (d *T{{$TModelName}}) ListWithPaginate(ctx context.Context, listOption *listoption.Paginate, whereOpts interface{}) ([]*{{$cliModelName}}, *listoption.Paginate, error) {
	var err error
	scope := d.newScope().Where(whereOpts)
	
    var {{$sModelName}}List []*{{$cliModelName}}
	var paginate *listoption.Paginate
	paginate, err = scope.PaginateQuery(listOption, &{{$sModelName}}List )
	if err != nil {
		log.Errorf("err: %v", err)
		return nil, nil, err
	}

	return {{$sModelName}}List , paginate, nil
}

{{end}}
`

func GenerateDAOContent(pd *ParseData) ([]byte, error) {

	temp := template.New("daoTemp")
	if funcMap != nil {
		temp.Funcs(funcMap)
	}
	temp, err := temp.Parse(daoTemp)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	var buffer bytes.Buffer
	err = temp.Execute(&buffer, pd)
	if err != nil {
		log.Printf("Can't generate file content,Error :%v\n", err)
		return nil, err
	}
	return buffer.Bytes(), nil
}
