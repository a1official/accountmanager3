// ✅ main.go (UPDATED)
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

type UserAccount struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ServerInfo struct {
	RootUsername string        `json:"root_username"`
	RootPassword string        `json:"root_password"`
	Accounts     []UserAccount `json:"accounts"`
}

var ipMap map[string]ServerInfo

func loadIPMap() error {
	file, err := os.Open("ipmap.json")
	if err != nil {
		ipMap = make(map[string]ServerInfo)
		return nil
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&ipMap)
	if err != nil {
		ipMap = make(map[string]ServerInfo)
	}
	return err
}

func saveIPMap() error {
	file, err := os.Create("ipmap.json")
	if err != nil {
		return err
	}
	defer file.Close()
	err = json.NewEncoder(file).Encode(ipMap)
	if err == nil {
		file.Sync()
	}
	return err
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/index.html"))
	tmpl.Execute(w, ipMap)
}

func addIPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		ip := strings.TrimSpace(r.FormValue("ip"))
		rootUser := strings.TrimSpace(r.FormValue("root_username"))
		rootPass := strings.TrimSpace(r.FormValue("root_password"))

		ipMap[ip] = ServerInfo{
			RootUsername: rootUser,
			RootPassword: rootPass,
			Accounts:     []UserAccount{},
		}
		saveIPMap()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func runRemoteCommand(ip, user, pass, script string) (string, error) {
	client, err := ssh.Dial("tcp", ip+":22", &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var output strings.Builder
	session.Stdout = &output
	session.Stderr = &output
	session.Stdin = strings.NewReader(script)
	err = session.Run("sh -s")
	return output.String(), err
}

func uploadCSVHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/upload.html"))
	tmpl.Execute(w, ipMap)
}

func createUsersHandler(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.FormValue("server_ip"))
	server, ok := ipMap[ip]
	if !ok {
		http.Error(w, "❌ IP not found in records", http.StatusBadRequest)
		fmt.Println("Received IP:", ip)
		fmt.Println("Available IPs:", ipMap)
		return
	}

	file, handler, err := r.FormFile("csvfile")
	if err != nil {
		http.Error(w, "Error reading file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	path := filepath.Join("uploads", handler.Filename)
	out, err := os.Create(path)
	if err != nil {
		http.Error(w, "Error saving file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	io.Copy(out, file)
	out.Close()

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "Failed to open uploaded CSV", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	reader := csv.NewReader(f)
	_, _ = reader.Read() // skip header

	var script strings.Builder
	var created []UserAccount
	var logBuilder strings.Builder

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if len(record) < 2 {
			logBuilder.WriteString(fmt.Sprintf("❌ Skipped invalid row: %v\n", record))
			continue
		}
		username := strings.TrimSpace(record[0])
		password := strings.TrimSpace(record[1])
		if username == "" || password == "" {
			logBuilder.WriteString(fmt.Sprintf("❌ Skipped empty fields: %v\n", record))
			continue
		}
		safePass := strings.ReplaceAll(password, `'`, `'\''`)
		// Using the working script that solves Ubuntu issues
		script.WriteString(fmt.Sprintf("echo '%s' | sudo -S useradd -m -s /bin/bash %s && echo '%s:%s' | sudo -S chpasswd\n", server.RootPassword, username, username, safePass))
		created = append(created, UserAccount{Username: username, Password: password})
	}

	if len(created) == 0 {
		logBuilder.WriteString("⚠️ No valid user entries found.\n")
	}

	output, err := runRemoteCommand(ip, server.RootUsername, server.RootPassword, script.String())
	if err != nil {
		logBuilder.WriteString("❌ Remote script execution failed:\n")
	}
	logBuilder.WriteString(output)

	s := ipMap[ip]
	s.Accounts = append(s.Accounts, created...)
	ipMap[ip] = s
	saveIPMap()

	tmpl := template.Must(template.ParseFiles("templates/logs.html"))
	tmpl.Execute(w, logBuilder.String())
}

func main() {
	os.MkdirAll("uploads", 0755)
	ipMap = make(map[string]ServerInfo)
	loadIPMap()

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/add-ip", addIPHandler)
	http.HandleFunc("/upload-csv", uploadCSVHandler)
	http.HandleFunc("/create-users", createUsersHandler)
	http.HandleFunc("/delete-csv", deleteCSVHandler)
	http.HandleFunc("/delete-users", deleteUsersHandler)
	http.HandleFunc("/delete-user", deleteSingleUserHandler)
	http.HandleFunc("/delete-selected", deleteSelectedUsersHandler)
	http.HandleFunc("/delete-all", deleteAllUsersHandler)

	// Excel functionality
	http.HandleFunc("/upload-excel", uploadExcelHandler)
	http.HandleFunc("/create-users-excel", createUsersFromExcelHandler)
	http.HandleFunc("/download-users", downloadUsersHandler)
	http.HandleFunc("/download-all-users", downloadAllUsersHandler)
	http.HandleFunc("/delete-excel", deleteExcelHandler)
	http.HandleFunc("/delete-users-excel", deleteUsersFromExcelHandler)

	// Software installation
	http.HandleFunc("/software", softwareHandler)
	http.HandleFunc("/install-software", installSoftwareHandler)

	fmt.Println(":8080")
	http.ListenAndServe(":8080", nil)
}
