package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"
	"github.com/robfig/cron/v3"
)

// Config 结构体定义配置文件格式
type Config struct {
	ScanDir            string `json:"scan_dir"`
	ResultDir          string `json:"result_dir"`
	OBSBucket          string `json:"obs_bucket"`
	OBSUrl             string `json:"obs_url"`
	OBSAccessKey       string `json:"obs_access_key"`
	OBSSecretKey       string `json:"obs_secret_key"`
	CronSchedule       string `json:"cron_schedule"`
	ScanTimeoutSecs    int    `json:"scan_timeout_secs"` // 新增：扫描超时时间(秒)
	GitleaksConfigPath string `json:"gitleaks_config_path"`
	ConfigPath         string `json:"-"` // 不导出到JSON
}

var appConfig Config

func main() {
	// 默认配置文件路径
	configPath := "config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// 加载配置
	if err := loadConfig(configPath); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 创建结果目录
	if err := os.MkdirAll(appConfig.ResultDir, 0750); err != nil {
		log.Fatalf("无法创建结果目录: %v", err)
	}

	// 设置定时任务
	c := cron.New(cron.WithParser(
		cron.NewParser(
			cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
		)))
	_, err := c.AddFunc(appConfig.CronSchedule, func() {
		log.Println("开始执行每日GitLeaks扫描任务...")
		if err := runDailyScan(); err != nil {
			log.Printf("扫描任务执行失败: %v", err)
		}
	})
	if err != nil {
		log.Fatalf("添加定时任务失败: %v", err)
	}

	c.Start()
	log.Printf("定时任务已启动，按照计划 '%s' 执行扫描...\n", appConfig.CronSchedule)
	log.Printf("每个文件夹扫描超时时间: %d秒\n", appConfig.ScanTimeoutSecs)

	// 保持程序运行
	select {}
}

func loadConfig(configPath string) error {
	// 读取配置文件
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %v", err)
	}

	// 解析JSON配置
	if err := json.Unmarshal(configData, &appConfig); err != nil {
		return fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 设置默认超时时间(如果未配置)
	if appConfig.ScanTimeoutSecs <= 0 {
		appConfig.ScanTimeoutSecs = 1800 // 默认5分钟
	}

	// 保存配置文件路径用于后续删除
	appConfig.ConfigPath = configPath

	// 删除配置文件
	//if err := os.Remove(configPath); err != nil {
	//	return fmt.Errorf("删除配置文件失败: %v", err)
	//}

	log.Println("配置已加载并删除配置文件")
	return nil
}

func runDailyScan() error {
	// 清理前一天的扫描结果
	if err := cleanupPreviousResults(); err != nil {
		return fmt.Errorf("清理前一天结果失败: %v", err)
	}

	// 获取所有需要扫描的目录
	projects, err := getProjectDirs(appConfig.ScanDir)
	if err != nil {
		return fmt.Errorf("获取目录列表失败: %v", err)
	}

	// 扫描每个目录
	for _, project := range projects {
		if err := scanAndUploadProjectWithTimeout(project, appConfig.GitleaksConfigPath); err != nil {
			log.Printf("目录 %s 处理失败: %v", project, err)
			continue
		}
	}

	return nil
}

func cleanupPreviousResults() error {
	// 删除结果目录下的所有文件
	dir, err := os.Open(appConfig.ResultDir)
	if err != nil {
		return err
	}
	defer func(dir *os.File) {
		err := dir.Close()
		if err != nil {
			err = fmt.Errorf("file close failed,%s", err.Error())
		}
	}(dir)

	names, err := dir.Readdirnames(-1)
	if err != nil {
		return err
	}

	for _, name := range names {
		if err := os.RemoveAll(filepath.Join(appConfig.ResultDir, name)); err != nil {
			return err
		}
	}

	log.Println("已清理前一天的扫描结果")
	return nil
}

func getProjectDirs(root string) ([]string, error) {
	var dirs []string

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}

	return dirs, nil
}

func scanAndUploadProjectWithTimeout(projectPath string, gitleaksConfigPath string) error {
	projectName := filepath.Base(projectPath)
	resultFile := filepath.Join(appConfig.ResultDir, fmt.Sprintf("result_%s.json", projectName))

	// 创建带有超时的context
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(appConfig.ScanTimeoutSecs)*time.Second)
	defer cancel()

	// 准备gitleaks命令
	var cmd *exec.Cmd
	if gitleaksConfigPath == "" {
		cmd = exec.CommandContext(ctx, "gitleaks", "dir", projectPath, "--report-path", resultFile)
	} else {
		cmd = exec.CommandContext(ctx, "gitleaks", "dir", projectPath, "-c", gitleaksConfigPath, "--report-path", resultFile)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("扫描超时(超过 %d 秒)", appConfig.ScanTimeoutSecs)
		}
		// 非超时就是扫出问题了，这里没开打印敏感信息
		fmt.Println("gitleaks执行失败: %v, 错误输出: %s", err, stderr.String())
	}

	log.Printf("目录 %s 扫描完成, 耗时: %v", projectName, duration)

	// 上传结果到OBS
	if err := uploadToOBS(resultFile); err != nil {
		return fmt.Errorf("上传到OBS失败: %v", err)
	}

	// 上传成功后删除本地文件
	if err := os.Remove(resultFile); err != nil {
		return fmt.Errorf("删除结果文件失败: %v", err)
	}

	log.Printf("目录 %s 处理完成: 已上传并删除本地结果文件", projectName)
	return nil
}

func uploadToOBS(filePath string) error {
	// 初始化OBS客户端
	client, err := obs.New(appConfig.OBSAccessKey, appConfig.OBSSecretKey, appConfig.OBSUrl)
	if err != nil {
		return err
	}

	// 读取文件内容
	fd, err := os.Open(filePath)
	if err != nil {
		return err
	}

	// 上传文件
	objectKey := fmt.Sprintf("jenkins/gitleaks_results/%s/%s", time.Now().Format("2006-01-02"), filepath.Base(filePath))
	input := &obs.PutObjectInput{}
	input.Bucket = appConfig.OBSBucket
	input.Key = objectKey
	input.Body = fd
	// 表示对象的过期时间（从对象最后修改时间开始计算），过期之后对象会被自动删除。
	//
	// 取值范围：
	//
	// 大于0的正整型，单位：天。
	input.Expires = 1
	_, err = client.PutObject(input)
	return err
}
