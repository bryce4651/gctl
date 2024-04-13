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
}

func checkProtoc() bool {
	p := exec.Command("protoc")
	if p.Run() != nil {
		log.Error("Please install protoc first and than rerun the command")
		log.Warn("See (https://github.com/ml444/gctl/README.md#install-protoc)")
		return false
	}
	return true
}
func GenerateProtobuf(pd *parser.ParseData, basePath string, needGenGrpcPb bool) error {
	var err error
	var args []string
	//var protocName string
	//var protoGenGoName string
	//switch runtime.GOOS {
	//case "windows":
	//	//protocName = "protoc.exe"
	//	protoGenGoName = "protoc-gen-go.exe"
	//default:
	//	//protocName = "protoc"
	//	protoGenGoName = "protoc-gen-go"
	//}
	goPath := os.Getenv("GOPATH")
	if goPath == "" {
		goPath = build.Default.GOPATH
	}
	for _, x := range []string{"protoc-gen-go", "protoc-gen-go-grpc", "protoc-gen-go-http", "protoc-gen-validate", "protoc-gen-openapi"} {
		protocBin := filepath.Join("exec", "bin", x)
		protocBinPath := filepath.Join(goPath, "bin", x)
		if err := extractFile(protocBin, protocBinPath); err != nil {
			panic(err)
		}
	}

	//protoGenGoPath := filepath.ToSlash(filepath.Join(goPath, "bin", protoGenGoName))
	//args = append(args, fmt.Sprintf("--plugin=protoc-gen-go=%s", protoGenGoPath))
	//args = append(args, fmt.Sprintf("--go_out=%s", filepath.ToSlash(basePath)))
	//args = append(args, fmt.Sprintf("--go-http_out=%s", filepath.ToSlash(basePath)))
	//args = append(args, fmt.Sprintf("--validate_out=lang=go:%s", filepath.ToSlash(basePath)))
	//args = append(args, fmt.Sprintf("--openapi_out=%s", filepath.ToSlash(basePath)))
	//if needGenGrpcPb {
	//	args = append(args, fmt.Sprintf("--go-grpc_out=%s", filepath.ToSlash(basePath)))
	//}
	inputExt := []string{
		// "--go_out=paths=source_relative:.",
		"--go_out=.",
		"--go-grpc_out=.",
		"--go-http_out=.",
		// "--validate_out=lang=go:.",
		//"--openapi_out=.",
		"--go-validate_out=.",
		"--go-errcode_out=.",
		"--go-gorm_out=.",
		fmt.Sprintf("--openapi_out=%s", filepath.ToSlash(basePath)),
	}
	args = append(args, inputExt...)
	// include proto
	//includePaths := getIncludePathList()
	//for _, x := range includePaths {
	//	args = append(args, fmt.Sprintf("--proto_path=%s", x))
	//}

	// 提取嵌入的protoc执行文件
	protocBin := "exec/protoc"
	protocBinPath := filepath.Join(os.TempDir(), "protoc")
	if err := extractFile(protocBin, protocBinPath); err != nil {
		panic(err)
	}

	// 赋予执行权限
	if err := os.Chmod(protocBinPath, 0755); err != nil {
		panic(err)
	}

	// 生成Proto文件的命令，假设您想生成go文件
	tempDir := os.TempDir()
	protosDir := filepath.Join(tempDir, "protos")
	//googleProtoDir := filepath.Join(tempDir, "google")

	if err := extractDirectory("protos", protosDir, ProtoFiles); err != nil {
		log.Error(err)
		return err
	}
	//protosDir := "protos"
	args = append(args, fmt.Sprintf("--proto_path=%s", protosDir))
	protoDir, protoName := filepath.Split(pd.FilePath)
	args = append(args, fmt.Sprintf("-I=%s", protoDir), protoName)
	cmd := exec.Command(protocBinPath, args...)
	cmd.Dir = "." // 设置工作目录

	// 设置环境变量，以便protoc找到所需的插件

	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH"))

	// protocPath := filepath.ToSlash(filepath.Join(goPath, "bin", protocName))
	// cmd := exec.Command(protocPath, args...)
	//cmd := exec.Command(protocName, args...)
	log.Info("exec:", cmd.String())

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err = cmd.Run()
	outStr := outBuf.String()
	errStr := errBuf.String()
	if err != nil {
		log.Infof("Err: %s \nStdout: %s \nStderr: %s", err, outStr, errStr)
		return err
	}
	if outStr != "" {
		log.Info("out:", outStr)
	}
	if errStr != "" {
		log.Error("err:", errStr)
	}
	return nil
}

func getIncludePathList() []string {
	return config.GlobalConfig.AllProtoPathList
}

func extractFile(src, dst string) error {
	if util.IsFileExist(dst) {
		log.Warn("file is exist: ", dst)
		return nil
	}
	srcFile, err := ExecFiles.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	return nil
}

func extractDirectory(src, dst string, fs embed.FS) error {
	files, err := fs.ReadDir(src)
	if err != nil {
		log.Error(err)
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			err = os.MkdirAll(filepath.Join(dst, file.Name()), os.ModePerm)
			if err != nil {
				log.Error(err)
				return err
			}
			err = extractDirectory(filepath.Join(src, file.Name()), filepath.Join(dst, file.Name()), fs)
			if err != nil {
				log.Error(err)
				return err
			}
			continue
		}
		srcFile, err := fs.Open(filepath.Join(src, file.Name()))
		if err != nil {
			log.Error(err)
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(filepath.Join(dst, file.Name()), os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Error(err)
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		if err != nil {
			log.Error(err)
			return err
		}
	}

	return nil
}
