package config_test

import "testing"

// isolateConfigEnv 把 env 文件的搜索根挪到临时目录。
//
// config.Load() 内部会调 LoadDotEnv()，候选包含 ./.env、../.env、<exe>/.env
// 与 ~/.codebuddy/proxy.env。不隔离时测试会读进开发者真实的 proxy.env
// （含 API Key 与开关），断言结果随本机文件变化，还会把其中的键泄漏到
// 后续测试的进程环境里。
//
// 返回临时 HOME，供需要自己 chdir 的测试拼路径。
func isolateConfigEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows 上 os.UserHomeDir 用它
	t.Chdir(home)
	return home
}
