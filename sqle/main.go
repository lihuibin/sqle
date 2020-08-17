package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"actiontech.cloud/universe/sqle/v3/sqle/utils"

	"actiontech.cloud/universe/sqle/v3/sqle/api"
	"actiontech.cloud/universe/sqle/v3/sqle/api/server"
	"actiontech.cloud/universe/sqle/v3/sqle/inspector"
	"actiontech.cloud/universe/sqle/v3/sqle/log"
	"actiontech.cloud/universe/sqle/v3/sqle/model"
	"actiontech.cloud/universe/sqle/v3/sqle/sqlserverClient"
	"gopkg.in/yaml.v2"

	_ "github.com/pingcap/tidb/types/parser_driver"
	"github.com/spf13/cobra"
)

var version string
var port int
var user string
var mysqlUser string
var mysqlPass string
var mysqlHost string
var mysqlPort string
var mysqlSchema string
var configPath string
var pidFile string
var debug bool
var autoMigrateTable bool
var logPath = "./logs"
var sqlServerParserServerHost = "127.0.0.1"
var sqlServerParserServerPort = "10001"

func main() {
	var rootCmd = &cobra.Command{
		Use:   "sqle",
		Short: "SQLe",
		Long:  "SQLe\n\nVersion:\n  " + version,
		Run: func(cmd *cobra.Command, args []string) {
			if err := run(cmd, args); nil != err {
				fmt.Println(err.Error())
				os.Exit(1)
			}
		},
	}
	rootCmd.PersistentFlags().IntVarP(&port, "port", "p", 10000, "http server port")
	rootCmd.PersistentFlags().StringVarP(&mysqlUser, "mysql-user", "", "sqle", "mysql user")
	rootCmd.PersistentFlags().StringVarP(&mysqlPass, "mysql-password", "", "sqle", "Please make mysql password encode to base64")
	rootCmd.PersistentFlags().StringVarP(&mysqlHost, "mysql-host", "", "localhost", "mysql host")
	rootCmd.PersistentFlags().StringVarP(&mysqlPort, "mysql-port", "", "3306", "mysql port")
	rootCmd.PersistentFlags().StringVarP(&mysqlSchema, "mysql-schema", "", "sqle", "mysql schema")
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "", "", "config file path")
	rootCmd.PersistentFlags().StringVarP(&pidFile, "pidfile", "", "", "pid file path")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "", false, "debug mode, print more log")
	rootCmd.PersistentFlags().BoolVarP(&autoMigrateTable, "auto-migrate-table", "", false, "auto migrate table if table model has changed")

	var createConfigFileCmd = &cobra.Command{
		Use:   "load",
		Short: "create config file using the filled in parameters",
		Long:  "create config file using the filled in parameters",
		Run: func(cmd *cobra.Command, args []string) {
			log.InitLogger(logPath)
			defer log.ExitLogger()
			log.Logger().Info("create config file using the filled in parameters")
			f, err := os.Create(configPath)
			if err != nil {
				log.Logger().Errorf("open %v file error :%v", configPath, err)
				return
			}
			fileContent := `
server:
 sqle_config:
  server_port: {{SERVER_PORT}}
  auto_migrate_table: {{AUTO_MIGRATE_TABLE}}
  debug_log: {{DEBUG}}
  log_path: './logs'
 db_config:
  mysql_cnf:
   mysql_host: '{{MYSQL_HOST}}'
   mysql_port: '{{MYSQL_PORT}}'
   mysql_user: '{{MYSQL_USER}}'
   mysql_password: '{{MYSQL_PASSWORD}}'
   mysql_schema: '{{MYSQL_SCHEMA}}'
  sql_server_cnf:
   sql_server_host:  
   sql_server_port: 
`
			mysqlPass, err = utils.DecodeString(mysqlPass)
			if err != nil {
				log.Logger().Errorf("decode mysql password to string error :%v", err)
				return
			}
			fileContent = strings.Replace(fileContent, "{{SERVER_PORT}}", strconv.Itoa(port), -1)
			fileContent = strings.Replace(fileContent, "{{MYSQL_HOST}}", mysqlHost, -1)
			fileContent = strings.Replace(fileContent, "{{MYSQL_PORT}}", mysqlPort, -1)
			fileContent = strings.Replace(fileContent, "{{MYSQL_USER}}", mysqlUser, -1)
			fileContent = strings.Replace(fileContent, "{{MYSQL_PASSWORD}}", mysqlPass, -1)
			fileContent = strings.Replace(fileContent, "{{MYSQL_SCHEMA}}", mysqlSchema, -1)
			fileContent = strings.Replace(fileContent, "{{AUTO_MIGRATE_TABLE}}", strconv.FormatBool(autoMigrateTable), -1)
			fileContent = strings.Replace(fileContent, "{{DEBUG}}", strconv.FormatBool(debug), -1)
			_, err = io.WriteString(f, fileContent)
			if nil != err {
				log.Logger().Errorf("write config file error :%v", err)
				return
			}
		},
	}
	rootCmd.AddCommand(createConfigFileCmd)

	rootCmd.Execute()
}

func run(cmd *cobra.Command, _ []string) error {

	mysqlPass, err := utils.DecodeString(mysqlPass)
	if err != nil {
		return fmt.Errorf("decode mysql password to string error : %v", err)
	}
	// if conf path is exist, load option from conf
	if configPath != "" {
		conf := model.Config{}
		b, err := ioutil.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("load config path: %s failed error :%v", configPath, err)
		}
		err = yaml.Unmarshal(b, &conf)
		if err != nil {
			fmt.Printf("yaml unmarshal error %v", err)
		}
		mysqlUser = conf.Server.DBCnf.MysqlCnf.User
		mysqlPass = conf.Server.DBCnf.MysqlCnf.Password
		mysqlHost = conf.Server.DBCnf.MysqlCnf.Host
		mysqlPort = conf.Server.DBCnf.MysqlCnf.Port
		mysqlSchema = conf.Server.DBCnf.MysqlCnf.Schema
		port = conf.Server.SqleCnf.SqleServerPort
		autoMigrateTable = conf.Server.SqleCnf.AutoMigrateTable
		debug = conf.Server.SqleCnf.DebugLog
		logPath = conf.Server.SqleCnf.LogPath
		sqlServerParserServerHost = conf.Server.DBCnf.SqlServerCnf.Host
		sqlServerParserServerPort = conf.Server.DBCnf.SqlServerCnf.Port
	}

	// init logger
	log.InitLogger(logPath)
	defer log.ExitLogger()

	log.Logger().Info("starting sqled server")

	if pidFile != "" {
		f, err := os.Create(pidFile)
		if err != nil {
			return err
		}
		fmt.Fprintf(f, "%d\n", os.Getpid())
		f.Close()
		defer func() {
			os.Remove(pidFile)
		}()
	}

	err = inspector.LoadPtTemplateFromFile("./scripts/pt-online-schema-change.template")
	if err != nil {
		return err
	}

	s, err := model.NewStorage(mysqlUser, mysqlPass, mysqlHost, mysqlPort, mysqlSchema, debug)
	if err != nil {
		return err
	}
	model.InitStorage(s)
	_ = sqlserverClient.InitClient(sqlServerParserServerHost, sqlServerParserServerPort)

	if autoMigrateTable {
		if err := s.AutoMigrate(); err != nil {
			return err
		}
		if err := s.CreateRulesIfNotExist(inspector.DefaultRules); err != nil {
			return err
		}
		if err := s.CreateDefaultTemplate(inspector.DefaultRules); err != nil {
			return err
		}
	}

	//todo temporary solution  DMP-4714
	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGUSR2)

	exitChan := make(chan struct{}, 0)
	server.InitSqled(exitChan)
	go api.StartApi(port, exitChan, logPath)

	select {
	case <-exitChan:
		//log.UserInfo(stage, "Beego exit unexpectly")
		//case sig := <-killChan:
		//
		//case syscall.SIGUSR2:
		//doesn't support graceful shutdown because beego uses its own graceful-way
		//
		//os.HaltIfShutdown(stage)
		//log.UserInfo(stage, "Exit by signal %v", sig)
	case <-killChan:

	}
	log.Logger().Info("stop sqled server")
	return nil
}