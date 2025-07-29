package main

import (
	"encoding/csv"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// uploadExcelHandler handles Excel file uploads for user creation
func uploadExcelHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/upload_excel.html"))
	tmpl.Execute(w, ipMap)
}

// createUsersFromExcelHandler processes Excel files to create users
func createUsersFromExcelHandler(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.FormValue("server_ip"))
	server, ok := ipMap[ip]
	if !ok {
		http.Error(w, "❌ IP not found in records", http.StatusBadRequest)
		fmt.Println("Received IP:", ip)
		fmt.Println("Available IPs:", ipMap)
		return
	}

	file, handler, err := r.FormFile("excelfile")
	if err != nil {
		http.Error(w, "Error reading file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Save the uploaded file
	path := filepath.Join("uploads", handler.Filename)
	out, err := os.Create(path)
	if err != nil {
		http.Error(w, "Error saving file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		http.Error(w, "Error copying file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	out.Close()

	// Open the Excel file
	xlsx, err := excelize.OpenFile(path)
	if err != nil {
		http.Error(w, "Error opening Excel file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer xlsx.Close()

	// Get the first sheet
	sheetName := xlsx.GetSheetName(0)
	rows, err := xlsx.GetRows(sheetName)
	if err != nil {
		http.Error(w, "Error reading Excel rows: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var script strings.Builder
	var created []UserAccount
	var logBuilder strings.Builder

	// Skip header row
	if len(rows) > 0 {
		rows = rows[1:]
	}

	for _, row := range rows {
		if len(row) < 2 {
			logBuilder.WriteString(fmt.Sprintf("❌ Skipped invalid row: %v\n", row))
			continue
		}

		username := strings.TrimSpace(row[0])
		rollNo := strings.TrimSpace(row[1])

		if username == "" || rollNo == "" {
			logBuilder.WriteString(fmt.Sprintf("❌ Skipped empty fields: %v\n", row))
			continue
		}

		// Generate password as name@rollno
		password := fmt.Sprintf("%s@%s", username, rollNo)
		safePass := strings.ReplaceAll(password, `'`, `'\''`)

		// Create a username without spaces for Linux compatibility
		linuxUsername := strings.ReplaceAll(username, " ", "_")

		// Using the working script that solves Ubuntu issues
		script.WriteString(fmt.Sprintf("echo '%s' | sudo -S useradd -m -s /bin/bash %s && echo '%s:%s' | sudo -S chpasswd\n", server.RootPassword, linuxUsername, linuxUsername, safePass))

		// Store the modified username in the accounts list
		created = append(created, UserAccount{Username: linuxUsername, Password: password})
	}

	if len(created) == 0 {
		logBuilder.WriteString("⚠️ No valid user entries found.\n")
	}

	output, err := runRemoteCommand(ip, server.RootUsername, server.RootPassword, script.String())
	if err != nil {
		logBuilder.WriteString(fmt.Sprintf("❌ Remote script execution failed: %v\n", err))
	}
	logBuilder.WriteString(output)

	s := ipMap[ip]
	s.Accounts = append(s.Accounts, created...)
	ipMap[ip] = s
	saveIPMap()

	tmpl := template.Must(template.ParseFiles("templates/logs.html"))
	tmpl.Execute(w, logBuilder.String())
}

// downloadUsersHandler generates and serves a CSV file with user accounts
func downloadUsersHandler(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		http.Error(w, "Server IP is required", http.StatusBadRequest)
		return
	}

	server, ok := ipMap[ip]
	if !ok {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Set headers for download
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("users_%s_%s.csv", strings.ReplaceAll(ip, ".", "_"), timestamp)
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// Create CSV writer
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	writer.Write([]string{"Username", "Password", "Server IP", "Notes"})

	// Write data
	for _, account := range server.Accounts {
		writer.Write([]string{account.Username, account.Password, ip, ""})
	}
}

// downloadAllUsersHandler generates and serves a CSV file with all user accounts
func downloadAllUsersHandler(w http.ResponseWriter, r *http.Request) {
	// Set headers for download
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("all_users_%s.csv", timestamp)
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// Create CSV writer
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	writer.Write([]string{"Username", "Password", "Server IP", "Notes"})

	// Write data
	for ip, server := range ipMap {
		for _, account := range server.Accounts {
			writer.Write([]string{account.Username, account.Password, ip, ""})
		}
	}
}
