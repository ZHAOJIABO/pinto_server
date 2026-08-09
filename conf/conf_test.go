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

// The models map is the one place where config keys are user-defined, and viper
// splits any key containing its delimiter: a key like "gemini-3.1-flash" would
// decode as "gemini-3". The api_key also lives only in the untracked override,
// so the merge must not drop the rest of the model entry.
func TestInitDecodesAIModelsAndMergesPerModelKey(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	baseConfig := filepath.Join(dir, "server.yaml")
	localConfig := filepath.Join(dir, "server.local.yaml")
	if err := os.WriteFile(baseConfig, []byte(`
ai_generation:
  default_model: "gpt-image-2"
  models:
    gpt-image-2:
      adapter: openai_image_edit
      base_url: "https://api.example.test"
      api_key: ""
      model: "gpt-image-2"
      options:
        size: "512x512"
    gemini-3-1-flash-image-preview:
      adapter: gemini_generate_content
      base_url: "https://api.example.test"
      api_key: ""
      model: "gemini-3.1-flash-image-preview"
      options:
        aspect_ratio: "1:1"
        image_size: "1K"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localConfig, []byte(`
ai_generation:
  models:
    gemini-3-1-flash-image-preview:
      api_key: "local-gemini-key"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Init(baseConfig); err != nil {
		t.Fatal(err)
	}

	models := GlobalConfig.AIGeneration.Models
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %#v", models)
	}
	if _, ok := models[GlobalConfig.AIGeneration.DefaultModel]; !ok {
		t.Fatalf("default_model %q is not a configured key", GlobalConfig.AIGeneration.DefaultModel)
	}

	gemini, ok := models["gemini-3-1-flash-image-preview"]
	if !ok {
		t.Fatalf("gemini model key was split or lost: %#v", models)
	}
	if gemini.APIKey != "local-gemini-key" {
		t.Errorf("api_key = %q, want the local override", gemini.APIKey)
	}
	// The upstream model name keeps its dots: only the map key is constrained.
	if gemini.Model != "gemini-3.1-flash-image-preview" {
		t.Errorf("model = %q", gemini.Model)
	}
	if gemini.Adapter != "gemini_generate_content" || gemini.BaseURL != "https://api.example.test" {
		t.Errorf("the override wiped sibling fields: %#v", gemini)
	}
	if gemini.Options["aspect_ratio"] != "1:1" || gemini.Options["image_size"] != "1K" {
		t.Errorf("options = %#v", gemini.Options)
	}
	if models["gpt-image-2"].Options["size"] != "512x512" {
		t.Errorf("gpt-image-2 options = %#v", models["gpt-image-2"].Options)
	}
}
