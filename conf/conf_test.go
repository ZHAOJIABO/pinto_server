package conf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestInitMergesUntrackedLocalOverride(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	baseConfig := filepath.Join(dir, "server.yaml")
	localConfig := filepath.Join(dir, "server.local.yaml")
	if err := os.WriteFile(baseConfig, []byte(`
server:
  mode: local
mysql:
  password: "base-mysql-password"
oss:
  endpoint: "https://oss-cn-beijing.aliyuncs.com"
  region: "cn-beijing"
  bucket: "pinto-test"
  access_key_id: "base-access-key"
  access_key_secret: "base-access-secret"
admin:
  jwt_secret: "base-secret"
  accounts: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localConfig, []byte(`
mysql:
  password: "local-mysql-password"
oss:
  access_key_id: "local-access-key"
  access_key_secret: "local-access-secret"
admin:
  jwt_secret: "local-secret"
  accounts:
    - username: "admin"
      password_hash: "hash"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Init(baseConfig); err != nil {
		t.Fatal(err)
	}

	if GlobalConfig.Admin.JWTSecret != "local-secret" {
		t.Fatalf("expected local admin secret, got %q", GlobalConfig.Admin.JWTSecret)
	}
	if len(GlobalConfig.Admin.Accounts) != 1 ||
		GlobalConfig.Admin.Accounts[0].Username != "admin" {
		t.Fatalf("expected local admin account, got %#v", GlobalConfig.Admin.Accounts)
	}
	if GlobalConfig.MySQL.Password != "local-mysql-password" {
		t.Fatalf("expected local MySQL password override")
	}
	if GlobalConfig.OSS.AccessKeyID != "local-access-key" ||
		GlobalConfig.OSS.AccessKeySecret != "local-access-secret" {
		t.Fatalf("expected local OSS credential override")
	}
}
