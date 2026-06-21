// Package provision builds and runs the SSH steps that stand up a new-meridian node.
package provision

import (
	"encoding/hex"
	"fmt"
)

// Step is one named provisioning command.
type Step struct {
	Key   string
	Label string
	Cmd   string
}

// Input parameterizes the provisioning script.
type Input struct {
	APIKey          string
	FingerprintSeed string
	SourceRepo      string
	InstallDir      string
}

// GenAPIKey builds a meridian API key: "sk-mer-" + 32 random bytes hex.
func GenAPIKey(rnd func([]byte)) string {
	b := make([]byte, 32)
	rnd(b)
	return "sk-mer-" + hex.EncodeToString(b)
}

// Steps returns the ordered provisioning commands for the target host.
func Steps(in Input) []Step {
	dir := in.InstallDir
	return []Step{
		{"ensure-docker", "安装/确认 Docker",
			"command -v docker >/dev/null 2>&1 || (curl -fsSL https://get.docker.com | sh); systemctl enable --now docker 2>/dev/null || true"},
		{"fetch-source", "拉取 new-meridian 源码",
			fmt.Sprintf("if [ -d '%s/.git' ]; then cd '%s' && git pull; else git clone '%s' '%s'; fi", dir, dir, in.SourceRepo, dir)},
		{"write-env", "写入 .env",
			fmt.Sprintf("cd '%s' && printf 'MERIDIAN_API_KEY=%s\\nMERIDIAN_HOST=0.0.0.0\\n' > .env", dir, in.APIKey)},
		{"build", "本机构建(指纹多样化)",
			fmt.Sprintf("cd '%s' && docker compose build --build-arg CACHEBUST='%s'", dir, in.FingerprintSeed)},
		{"compose-up", "启动容器",
			fmt.Sprintf("cd '%s' && docker compose up -d", dir)},
		{"read-key", "确认 API key",
			fmt.Sprintf("echo '%s'", in.APIKey)},
	}
}
