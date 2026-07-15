// Command admin-password-hash creates a PBKDF2-SHA256 hash for the internal
// Flutter Web administrator configuration. It reads the password from stdin so
// a password does not need to appear in the command history.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/zhaojiabo/bobobeads_server/internal/service/admin"
)

func main() {
	password, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取密码失败:", err)
		os.Exit(1)
	}
	password = strings.TrimRight(password, "\r\n")
	hash, err := admin.HashPassword(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成密码哈希失败:", err)
		os.Exit(1)
	}
	fmt.Println(hash)
}
