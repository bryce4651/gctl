package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/emicklei/proto"

	"github.com/ml444/gctl/config"

	"github.com/ml444/gctl/util"

	log "github.com/ml444/glog"
	"github.com/spf13/cobra"

	"github.com/ml444/gctl/parser"
)

var serverCmd = &cobra.Command{
	Use:     "server",
	Short:   "Generate server lib",
	Aliases: []string{"s"},
	Run: func(_ *cobra.Command, args []string) {
		var err error
		if protoPath == "" && len(args) == 0 {
			log.Error("You must provide the file of proto: gctl server -p=<protoFilepath> or gctl server <NAME>")
			return
		}
		if protoPath == "" {
			arg := args[0]
			protoPath = filepath.Join("pkg", args[0], fmt.Sprintf("%s.proto", arg))
		}
		if serviceGroup == "" && config.GlobalConfig.DefaultSvcGroup != "" {
			serviceGroup = config.GlobalConfig.DefaultSvcGroup
		}

		serviceName := getServiceName(protoPath)
		//protoPath = config.GetTargetProtoAbsPath(serviceGroup, protoPath)
		//baseDir := config.GlobalConfig.TargetRootPath
		onceFiles := config.GlobalConfig.OnceFiles
		onceFileMap := map[string]bool{}
		for _, fileName := range onceFiles {
			onceFileMap[fileName] = true
		}
		pd, err := parser.ParseProtoFile(protoPath)
		if err != nil {
			log.Errorf("err: %v", err)
			return
		}
		serverRootDir, _ := os.Getwd()
		_, projectName := filepath.Split(serverRootDir)
		pd.RootFolder = projectName
		pd.ModulePrefix = config.JoinModulePrefixWithGroup(serviceGroup)
		pd.ModulePath = strings.Join([]string{pd.ModulePrefix, projectName}, "/")
		pd.ServerImport = strings.Join([]string{pd.ModulePath, "server", fmt.Sprintf("%sserver", serviceName)}, "/")
		pd.ClientImport = strings.Join([]string{pd.ModulePath, "pkg", serviceName}, "/")
		if config.GlobalConfig.EnableAssignPort {
			var port int
			svcAssign := util.NewSvcAssign(serviceName, serviceGroup)
			err = svcAssign.GetOrAssignPortAndErrcode(&port, nil)
			if err != nil {
				log.Error(err)
				return
			}
			if port != 0 {
				var ports []int
				for i := 0; i < config.GlobalConfig.SvcPortInterval; i++ {
					ports = append(ports, port+i)
				}
				pd.Ports = ports
			}
		}
		//protoTempPath := config.GetTempProtoAbsPath()
		serverTempDir := "templates/server"
		// serverRootDir := filepath.Join(baseDir, fmt.Sprintf("%sServer", strings.Split(pd.Options["go_package"], ";")[0]))
		log.Debug("server root dir:", serverRootDir)
		log.Debug("template root dir:", serverTempDir)
		if !util.IsFileExist(filepath.Join(serverRootDir, "cmd", "main.go")) {
			projectPath := "templates/project"
			err = fs.WalkDir(TemplateProject, projectPath, func(path string, info fs.DirEntry, err error) error {
				if err != nil {
					log.Errorf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
					return err
				}
				if info.IsDir() {
					log.Debugf("skipping a dir without errors: %+v \n", info.Name())
					return nil
				}

				fileName := strings.TrimSuffix(info.Name(), config.GetTempFilesFormatSuffix())
				parentPath := strings.TrimSuffix(strings.TrimPrefix(path, projectPath), info.Name())
				targetFile := serverRootDir + parentPath + fileName
				targetFile = strings.ReplaceAll(targetFile, config.ServiceNameVar, serviceName)
				if util.IsFileExist(targetFile) && onceFileMap[fileName] {
					log.Printf("[%s] file is exist in this directory, skip it", targetFile)
					return nil
				}

				log.Infof("generating file: %s", targetFile)
				err = parser.GenerateTemplate(targetFile, TemplateProject, path, pd)
				if err != nil {
					return err
				}
				return nil
			})
			if err != nil {
				fmt.Printf("error walking the path %q: %v\n", serverTempDir, err)
				return
			}
		}
		err = fs.WalkDir(TemplateServer, serverTempDir, func(path string, info fs.DirEntry, err error) error {
			if err != nil {
				log.Errorf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
				return err
			}
			if info.IsDir() {
				log.Debugf("skipping a dir without errors: %+v \n", info.Name())
				return nil
			}

			fileName := strings.TrimSuffix(info.Name(), config.GetTempFilesFormatSuffix())
			parentPath := strings.TrimSuffix(strings.TrimPrefix(path, serverTempDir), info.Name())
			targetFile := serverRootDir + parentPath + fileName
			targetFile = strings.ReplaceAll(targetFile, config.ServiceNameVar, serviceName)
			if util.IsFileExist(targetFile) {
				if onceFileMap[fileName] {
					log.Warnf("[%s] file is exist in this directory, skip it", targetFile)
					return nil
				}
				if fileName == "service.go" {
					pd.ParseGoFile(targetFile)
					var newSvcList []*parser.Service
					var newSvcNameList []string
					existServiceMap := pd.ServiceMethodMap
					log.Info("existServiceMap: ", existServiceMap)
					if len(pd.ServiceMethodMap) == 0 {
						log.Error("service.go文件异常， 请手动删除后，重新执行")
						return errors.New("service.go文件异常， 请手动删除后，重新执行")
					}
					for _, svc := range pd.ServiceList {
						s := parser.Service{
							ServiceName: svc.ServiceName,
						}
						log.Info("svc.ServiceName: ", svc.ServiceName)
						methodMap, ok := existServiceMap[util.ToUpperFirst(svc.ServiceName)+"Service"]
						if !ok {
							newSvcNameList = append(newSvcNameList, svc.ServiceName)

							for _, rpc := range svc.RpcList {
								s.RpcList = append(s.RpcList, &parser.RpcMethod{
									Name:         rpc.Name,
									RequestType:  rpc.RequestType,
									ResponseType: rpc.ResponseType,
								})
							}
						} else {
							for _, rpc := range svc.RpcList {
								_, ok1 := methodMap[rpc.Name]
								if ok1 {
									continue
								}
								s.RpcList = append(s.RpcList, &parser.RpcMethod{
									Name:         rpc.Name,
									RequestType:  rpc.RequestType,
									ResponseType: rpc.ResponseType,
								})
							}
						}
						newSvcList = append(newSvcList, &s)
					}
					if len(newSvcNameList) > 0 {
						log.Warn("暂时不能处理多service的情况， 请手动处理")
						return nil
					}
					var file *os.File
					file, err = os.OpenFile(targetFile, os.O_APPEND|os.O_WRONLY, 0644)
					if err != nil {
						log.Errorf("open file err: %v", err)
						return err
					}
					defer file.Close()
					// content, err := io.ReadAll(file)
					// if err != nil {
					// 	log.Errorf("read file err: %v", err)
					// 	return err
					// }

					// 替换操作
					oldServiceList := pd.ServiceList
					pd.ServiceList = newSvcList
					defer func() {
						pd.ServiceList = oldServiceList
					}()

					_, err = file.Seek(0, io.SeekEnd)
					if err != nil {
						log.Error("无法移动文件指针:", err)
						return err
					}
					// 模板生成新增的内容
					var newBuf []byte
					newBuf, err = parser.GenerateServiceMethodContent(pd)
					if err != nil {
						log.Errorf("generate service method content err: %v", err)
						return err
					}
					// content = append(content, newBuf...)

					_, err = file.Write(newBuf)
					if err != nil {
						log.Errorf("write file err: %v", err)
						return err
					}

				}

				if fileName == "dao.go" {
					pd.ParseGoFile(targetFile)
					existModelMap := pd.ObjectMap
					log.Debug("existModelMap: ", existModelMap)
					var newModelList []*proto.Message
					for _, model := range pd.ModelList {
						kind, ok := existModelMap[fmt.Sprintf("T%s", strings.TrimPrefix(model.Name, "Model"))]
						if ok && kind == "type" {
							continue
						}

						newModelList = append(newModelList, model)
					}
					if len(newModelList) == 0 {
						log.Warn("dao文件无Model新增")
						return nil
					}
					var file *os.File
					file, err = os.OpenFile(targetFile, os.O_WRONLY|os.O_APPEND, 0644)
					if err != nil {
						log.Errorf("open file err: %v", err)
						return err
					}
					defer file.Close()
					// content, err := io.ReadAll(file)
					// if err != nil {
					// 	log.Errorf("read file err: %v", err)
					// 	return err
					// }

					// 替换操作
					oldModelList := pd.ModelList
					pd.ModelList = newModelList
					defer func() {
						pd.ModelList = oldModelList
					}()
					_, err = file.Seek(0, io.SeekEnd)
					if err != nil {
						log.Error("无法移动文件指针:", err)
						return err
					}

					// 模板生成新增的内容
					var newBuf []byte
					newBuf, err = parser.GenerateDAOContent(pd)
					if err != nil {
						log.Errorf("generate service method content err: %v", err)
						return err
					}
					// content = append(content, newBuf...)
					_, err = file.Write(newBuf)
					if err != nil {
						log.Errorf("write file err: %v", err)
						return err
					}

				}
			} else {
				log.Infof("generating file: %s", targetFile)
				err = parser.GenerateTemplate(targetFile, TemplateServer, path, pd)
				if err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			fmt.Printf("error walking the path %q: %v\n", serverTempDir, err)
			return
		}

		// go mod tidy && go fmt
		{
			util.CmdExec("cd " + serverRootDir + " && go fmt ./...")
		}
	},
}
