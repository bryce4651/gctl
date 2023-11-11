package cmd

import (
	"bytes"
	"embed"
	"fmt"
	"go/build"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ml444/gutil/osx"

	"github.com/ml444/gctl/config"
	"github.com/ml444/gctl/util"

	log "github.com/ml444/glog"
	"github.com/spf13/cobra"

	"github.com/ml444/gctl/parser"
)

var clientCmd = &cobra.Command{
	Use:     "client",
	Short:   "Generate client lib",
	Aliases: []string{"c"},
	Run: func(_ *cobra.Command, args []string) {

		if protoPath == "" && len(args) == 0 {
			log.Error("You must provide the file of proto: gctl client -p=<protoFilepath> or gctl client <NAME>")
			return
		}
		if serviceGroup == "" && config.GlobalConfig.DefaultSvcGroup != "" {
			serviceGroup = config.GlobalConfig.DefaultSvcGroup
		}
		//baseDir := config.GlobalConfig.TargetRootPath
		if protoPath == "" {
			arg := args[0]
			protoPath = filepath.Join("pkg", args[0], fmt.Sprintf("%s.proto", arg))
			//protoPath = config.GetTargetProtoAbsPath(serviceGroup, protoPath)
			//protoPath = filepath.Join(baseDir, config.GlobalConfig.GoModulePrefix, fmt.Sprintf("%s.proto", arg))
		}
		//tmpDir := config.GetTempClientAbsDir()
		tmpDir := "templates/client"
		onceFiles := config.GlobalConfig.OnceFiles
		//log.Debug("root location of code generation: ", baseDir)
		log.Debug("template path of code generation: ", tmpDir)
		log.Debug("files that are executed only once during initialization:", onceFiles)
		onceFileMap := map[string]bool{}
		for _, fileName := range onceFiles {
			onceFileMap[fileName] = true
		}
		var err error
		pd, err := parser.ParseProtoFile(protoPath)
		if err != nil {
			log.Errorf("err: %v", err)
			return
		}
		serviceName := getServiceName(protoPath)
		if config.GlobalConfig.EnableAssignErrcode {
			var moduleId int
			svcAssign := util.NewSvcAssign(serviceName, serviceGroup)
			moduleId, err = svcAssign.GetModuleId()
			if err != nil {
				log.Error(err)
				return
			}
			pd.ModuleId = moduleId
		}
		clientRootDir, _ := os.Getwd()
		if pkgPath := pd.Options["go_package"]; pkgPath != "" {
			if strings.Contains(pkgPath, ";") {
				pkgPath = strings.Split(pkgPath, ";")[0]
			}
			clientRootDir = filepath.Join(clientRootDir, pkgPath)
		} else {
			//clientRootDir = config.GetTargetClientAbsDir(serviceGroup, serviceName)
		}
		err = fs.WalkDir(TemplateClient, tmpDir, func(path string, info fs.DirEntry, err error) error {
			if err != nil {
				log.Errorf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
				return err
			}
			if info.IsDir() {
				log.Warnf("skipping dir: %+v \n", info.Name())
				return nil
			}
			fileName := strings.TrimSuffix(info.Name(), config.GetTempFilesFormatSuffix())
			parentPath := strings.TrimRight(strings.TrimPrefix(path, tmpDir), info.Name())
			targetFile := clientRootDir + parentPath + fileName
			if util.IsFileExist(targetFile) && onceFileMap[fileName] {
				log.Warnf("[%s] file is exist in this directory, skip it", targetFile)
				return nil
			}

			log.Infof("generating file: %s \n", targetFile)
			err = parser.GenerateTemplate(targetFile, TemplateClient, path, pd)
			if err != nil {
				log.Error(err)
				return err
			}
			return nil
		})
		if err != nil {
			log.Errorf("error walking the path %q: %v", tmpDir, err)
			return
		}

		// generate protobuf file
		{
			if ok := checkProtoc(); !ok {
				return
			}
			log.Info("generating protobuf file")
			err = GenerateProtobuf(pd, clientRootDir, needGenGrpcPb)
			if err != nil {
				log.Error(err)
				return
			}
		}

		absPath, err := filepath.Abs(clientRootDir)
		if err != nil {
			log.Errorf("err: %v", err)
			return
		}

		// inject tag
		{
			pbFilepath := filepath.Join(clientRootDir, fmt.Sprintf("%s.pb.go", serviceName))
			areas, err := parser.ParsePbFile(pbFilepath, nil, nil)
			if err != nil {
				log.Fatal(err)
			}
			if err = parser.WritePbFile(pbFilepath, areas, false); err != nil {
				log.Fatal(err)
			}
		}

		// go mod tidy && go fmt
		if osx.IsFileExist(filepath.Join(absPath, "go.mod")) {
			util.CmdExec("cd " + absPath + " && go mod tidy")
		}
		util.CmdExec("cd " + absPath + " && go fmt ./...")
	},
}

func getServiceName(protoPath string) string {
	_, fname := filepath.Split(protoPath)
	return strings.TrimSuffix(fname, ".proto")
