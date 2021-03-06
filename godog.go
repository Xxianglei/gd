/**
 * Copyright 2018 gd Author. All Rights Reserved.
 * Author: Xxianglei
 */

package gd

import (
	"fmt"
	"github.com/Xxianglei/gd/net/dgrpc"
	"github.com/Xxianglei/gd/net/dhttp"
	"github.com/Xxianglei/gd/net/dogrpc"
	"github.com/Xxianglei/gd/runtime/helper"
	"github.com/Xxianglei/gd/runtime/pc"
	"github.com/Xxianglei/gd/runtime/stat"
	"github.com/Xxianglei/gd/utls"
	"google.golang.org/grpc"
	"runtime"
	"syscall"
	"time"
)

type Engine struct {
	HttpServer *dhttp.HttpServer
	RpcServer  *dogrpc.RpcServer
	GrpcServer *dgrpc.GrpcServer
}

func Default() *Engine {
	e := &Engine{
		HttpServer: &dhttp.HttpServer{
			NoGinLog: true,
		},
		RpcServer:  dogrpc.NewDogRpcServer(),
		GrpcServer: &dgrpc.GrpcServer{},
	}

	InitLog()
	return e
}

func InitLog() {
	enable := Config("Log", "enable").MustBool(false)
	if enable {
		var port int
		if Config("Server", "httpPort").MustInt() > 0 {
			port = Config("Server", "httpPort").MustInt()
		} else if Config("Server", "rpcPort").MustInt() > 0 {
			port = Config("Server", "rpcPort").MustInt()
		} else if Config("Server", "grpcPort").MustInt() > 0 {
			port = Config("Server", "grpcPort").MustInt()
		}

		if err := restoreLogConfig("", Config("Server", "serverName").String(),
			port, Config("Log", "level").String(), Config("Log", "logDir").String()); err != nil {
			panic(fmt.Sprintf("restoreLogConfig occur error:%v", err))
		}

		LoadConfiguration(logConfigFile)
	}
}

// Engine Run
func (e *Engine) Run() error {
	Info("- - - - - - - - - - - - - - - - - - -")
	Info("process start")
	// register signal
	e.Signal()

	// dump when error occurs
	logDir := Config("Log", "logDir").String()
	file, err := utls.Dump(logDir, Config("Server", "serverName").String())
	if err != nil {
		Error("Error occurs when initialize dump dumpPanic file, error = %s", err.Error())
	}

	// output exit info
	defer func() {
		Info("server stop...code: %d", runtime.NumGoroutine())
		time.Sleep(time.Second)
		Info("server stop...ok")
		Info("- - - - - - - - - - - - - - - - - - -")
		if err := utls.ReviewDumpPanic(file); err != nil {
			Error("Failed to review dump dumpPanic file, error = %s", err.Error())
		}
	}()

	// init cpu and memory
	err = e.initCPUAndMemory()
	if err != nil {
		Error("Cannot init CPU and memory module, error = %s", err.Error())
		return err
	}

	// init falcon
	falconEnable := Config("Statistics", "falcon").MustBool(false)
	if falconEnable {
		pc.Init()
		defer pc.ClosePerfCounter()
	}

	// init stat
	statEnable := Config("Statistics", "stat").MustBool(false)
	if statEnable {
		statInterval := Config("Statistics", "statInterval").MustInt64(5)
		statFile := "stat.log"
		if logDir != "" {
			statFile = logDir + "/stat.log"
		}
		stat.StatMgrInstance().Init(statFile, time.Second*time.Duration(statInterval))
	}

	// http server
	httpPort := Config("Server", "httpPort").MustInt()
	if httpPort > 0 {
		Info("http server try listen port:%d", httpPort)

		e.HttpServer.HttpServerRunHost = fmt.Sprintf(":%d", httpPort)
		if err = e.HttpServer.Run(); err != nil {
			Error("Http server occur error in running application, error = %s", err.Error())
			return err
		}
		defer e.HttpServer.Stop()

		if falconEnable {
			pc.SetRunPort(httpPort)
		}
	}

	// grpc server
	grpcPort := Config("Server", "grpcPort").MustInt()
	if grpcPort > 0 {
		Info("grpc server try listen port:%d", grpcPort)

		e.GrpcServer.GrpcRunPort = grpcPort
		e.GrpcServer.ServiceName = Config("Server", "serverName").String()
		if err = e.GrpcServer.Run(); err != nil {
			Error("Grpc server occur error in running application, error = %s", err.Error())
			return err
		}
		defer e.GrpcServer.Stop()

		if falconEnable {
			pc.SetRunPort(grpcPort)
		}
	}

	// health
	healthPort := Config("Process", "healthPort").MustInt()
	if healthPort > 0 {
		Info("health server try listen port:%d", healthPort)

		host := fmt.Sprintf(":%d", healthPort)
		health := &helper.Helper{Host: host}
		if err := health.Start(); err != nil {
			Error("start health failed on %s\n", host)
			return err
		}
		defer health.Close()
	}

	// rpc server
	rpcPort := Config("Server", "rpcPort").MustInt()
	if rpcPort > 0 {
		Info("rpc server try listen port:%d", rpcPort)

		if err = e.RpcServer.Run(rpcPort); err != nil {
			Error("rpc server occur error in running application, error = %s", err.Error())
			return err
		}
		defer e.RpcServer.Stop()
	}

	<-Running
	return nil
}

func (e *Engine) initCPUAndMemory() error {
	maxCPU := Config("Process", "maxCPU").MustInt()
	numCpus := runtime.NumCPU()
	if maxCPU <= 0 {
		if numCpus > 3 {
			maxCPU = numCpus / 2
		} else {
			maxCPU = 1
		}
	} else if maxCPU > numCpus {
		maxCPU = numCpus
	}
	runtime.GOMAXPROCS(maxCPU)

	if Config("Process", "maxMemory").String() != "" {
		maxMemory, err := utls.ParseMemorySize(Config("Process", "maxMemory").String())
		if err != nil {
			Crash(fmt.Sprintf("conf field illgeal, max_memory:%s, error:%s", Config("Process", "maxMemory").String(), err.Error()))
		}

		var rlimit syscall.Rlimit
		syscall.Getrlimit(syscall.RLIMIT_AS, &rlimit)
		Info("old rlimit mem:%v", rlimit)
		rlimit.Cur = uint64(maxMemory)
		rlimit.Max = uint64(maxMemory)
		err = syscall.Setrlimit(syscall.RLIMIT_AS, &rlimit)
		if err != nil {
			Crash(fmt.Sprintf("syscall Setrlimit fail, rlimit:%v, error:%s", rlimit, err.Error()))
		} else {
			syscall.Getrlimit(syscall.RLIMIT_AS, &rlimit)
			Info("new rlimit mem:%v", rlimit)
		}
	}

	return nil
}

func (e *Engine) SetHttpServer(init dhttp.HttpServerIniter) {
	e.HttpServer.SetInit(init)
}

func (e *Engine) SetGrpcServer(init dgrpc.IRegisterHandler) {
	e.GrpcServer.Register(init)
}

// timeout Millisecond
func NewRpcClient(timeout time.Duration, retryNum uint32) *dogrpc.RpcClient {
	client := dogrpc.NewClient(timeout, retryNum)
	return client
}

func NewHttpClient(Timeout time.Duration, Domain string) *dhttp.HttpClient {
	client := &dhttp.HttpClient{
		Timeout: Timeout,
		Domain:  Domain,
	}
	if err := client.Start(); err != nil {
		Error("http client start occur error:%s", err.Error())
		return nil
	}
	return client
}

func NewGrpcClient(target string, makeRawClient func(conn *grpc.ClientConn) (interface{}, error), serviceName string) *dgrpc.GrpcClient {
	client := &dgrpc.GrpcClient{
		Target:      target,
		ServiceName: serviceName,
	}

	if err := client.Start(makeRawClient); err != nil {
		Error("grpc client start occur error:%s", err.Error())
		return nil
	}
	return client
}
