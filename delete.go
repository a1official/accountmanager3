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

	"github.com/xuri/excelize/v2"
)

// deleteCSVHandler renders the delete form template
func deleteCSVHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/delete.html"))
	tmpl.Execute(w, ipMap)
}

// deleteUsersHandler processes the CSV file and deletes users from the server
func deleteUsersHandler(w http.ResponseWriter, r *http.Request) {
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
	var deleted []string
	var logBuilder strings.Builder

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if len(record) < 1 {
			logBuilder.WriteString(fmt.Sprintf("❌ Skipped invalid row: %v\n", record))
			continue
		}
		username := strings.TrimSpace(record[0])
		if username == "" {
			logBuilder.WriteString("❌ Skipped empty username\n")
			continue
		}

		// Delete user and their home directory
		script.WriteString(fmt.Sprintf("echo '%s' | sudo -S userdel -r %s 2>/dev/null || echo 'User %s not found or already deleted'\n",
			server.RootPassword, username, username))
		deleted = append(deleted, username)
	}

	if len(deleted) == 0 {
		logBuilder.WriteString("⚠️ No valid user entries found.\n")
	}

	output, err := runRemoteCommand(ip, server.RootUsername, server.RootPassword, script.String())
	if err != nil {
		logBuilder.WriteString(fmt.Sprintf("❌ Remote script execution failed: %v\n", err))
	}
	logBuilder.WriteString(output)

	// Remove deleted users from accounts list
	var updatedAccounts []UserAccount
	for _, account := range server.Accounts {
		found := false
		for _, deletedUser := range deleted {
			if account.Username == deletedUser {
				found = true
				break
			}
		}
		if !found {
			updatedAccounts = append(updatedAccounts, account)
		}
	}

	server.Accounts = updatedAccounts
	ipMap[ip] = server
	saveIPMap()

	tmpl := template.Must(template.ParseFiles("templates/logs.html"))
	tmpl.Execute(w, logBuilder.String())
}

// deleteSingleUserHandler deletes a single user from the server
func deleteSingleUserHandler(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.FormValue("server_ip"))
	username := strings.TrimSpace(r.FormValue("username"))

	if username == "" {
		http.Error(w, "❌ Username is required", http.StatusBadRequest)
		return
	}

	server, ok := ipMap[ip]
	if !ok {
		http.Error(w, "❌ IP not found in records", http.StatusBadRequest)
		return
	}

	script := fmt.Sprintf("echo '%s' | sudo -S userdel -r %s 2>/dev/null || echo 'User %s not found or already deleted'",
		server.RootPassword, username, username)

	output, err := runRemoteCommand(ip, server.RootUsername, server.RootPassword, script)

	var logBuilder strings.Builder
	if err != nil {
		logBuilder.WriteString(fmt.Sprintf("❌ Remote script execution failed: %v\n", err))
	}
	logBuilder.WriteString(output)

	// Remove user from accounts list
	var updatedAccounts []UserAccount
	for _, account := range server.Accounts {
		if account.Username != username {
			updatedAccounts = append(updatedAccounts, account)
		}
	}

	server.Accounts = updatedAccounts
	ipMap[ip] = server
	saveIPMap()

	tmpl := template.Must(template.ParseFiles("templates/logs.html"))
	tmpl.Execute(w, logBuilder.String())
}

// deleteSelectedUsersHandler deletes multiple selected users from the server
func deleteSelectedUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get server IP
	ip := strings.TrimSpace(r.FormValue("server_ip"))
	if ip == "" {
		http.Error(w, "Server IP is required", http.StatusBadRequest)
		return
	}

	// Get server info
	server, ok := ipMap[ip]
	if !ok {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Get selected usernames
	selectedUsers := r.Form["selected_users"]
	if len(selectedUsers) == 0 {
		http.Redirect(w, r, "/?msg=No+users+selected", http.StatusSeeOther)
		return
	}

	// Build script to delete all selected users
	var script strings.Builder
	var logBuilder strings.Builder

	logBuilder.WriteString(fmt.Sprintf("🗑️ Deleting %d selected users from %s\n\n", len(selectedUsers), ip))

	for _, username := range selectedUsers {
		script.WriteString(fmt.Sprintf("echo '%s' | sudo -S userdel -r %s 2>/dev/null || echo 'User %s not found or already deleted'\n",
			server.RootPassword, username, username))
	}

	// Execute the script
	output, err := runRemoteCommand(ip, server.RootUsername, server.RootPassword, script.String())
	if err != nil {
		logBuilder.WriteString(fmt.Sprintf("❌ Remote script execution failed: %v\n", err))
	}
	logBuilder.WriteString(output)

	// Remove deleted users from accounts list
	var updatedAccounts []UserAccount
	for _, account := range server.Accounts {
		found := false
		for _, username := range selectedUsers {
			if account.Username == username {
				found = true
				break
			}
		}
		if !found {
			updatedAccounts = append(updatedAccounts, account)
		}
	}

	server.Accounts = updatedAccounts
	ipMap[ip] = server
	saveIPMap()

	// Show logs
	tmpl := template.Must(template.ParseFiles("templates/logs.html"))
	tmpl.Execute(w, logBuilder.String())
}

// deleteAllUsersHandler deletes all users from a specific server
func deleteAllUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get server IP
	ip := strings.TrimSpace(r.FormValue("server_ip"))
	if ip == "" {
		http.Error(w, "Server IP is required", http.StatusBadRequest)
		return
	}

	// Get server info
	server, ok := ipMap[ip]
	if !ok {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Check if there are any users to delete
	if len(server.Accounts) == 0 {
		http.Redirect(w, r, "/?msg=No+users+to+delete", http.StatusSeeOther)
		return
	}

	// Build script to delete all users
	var script strings.Builder
	var logBuilder strings.Builder

	logBuilder.WriteString(fmt.Sprintf("🗑️ Deleting ALL %d users from server %s\n\n", len(server.Accounts), ip))
	logBuilder.WriteString("Users being deleted:\n")

	// Add each user to the deletion script
	for _, account := range server.Accounts {
		script.WriteString(fmt.Sprintf("echo '%s' | sudo -S userdel -r %s 2>/dev/null || echo 'User %s not found or already deleted'\n",
			server.RootPassword, account.Username, account.Username))
		logBuilder.WriteString(fmt.Sprintf("- %s\n", account.Username))
	}

	logBuilder.WriteString("\nExecution Log:\n")

	// Execute the script
	output, err := runRemoteCommand(ip, server.RootUsername, server.RootPassword, script.String())
	if err != nil {
		logBuilder.WriteString(fmt.Sprintf("❌ Remote script execution failed: %v\n", err))
	}
	logBuilder.WriteString(output)

	// Clear all accounts from the server
	server.Accounts = []UserAccount{}
	ipMap[ip] = server
	saveIPMap()

	logBuilder.WriteString(fmt.Sprintf("\n✅ All users have been deleted from server %s\n", ip))

	// Show logs
	tmpl := template.Must(template.ParseFiles("templates/logs.html"))
	tmpl.Execute(w, logBuilder.String())
}

// deleteExcelHandler renders the delete from Excel form template
func deleteExcelHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/delete_excel.html"))
	tmpl.Execute(w, ipMap)
}

// deleteUsersFromExcelHandler processes Excel file and deletes users from the server
func deleteUsersFromExcelHandler(w http.ResponseWriter, r *http.Request) {
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
	var deleted []string
	var logBuilder strings.Builder

	// Skip header row
	if len(rows) > 0 {
		rows = rows[1:]
	}

	for _, row := range rows {
		if len(row) < 1 {
			logBuilder.WriteString(fmt.Sprintf("❌ Skipped invalid row: %v\n", row))
			continue
		}

		username := strings.TrimSpace(row[0])
		if username == "" {
			logBuilder.WriteString("❌ Skipped empty username\n")
			continue
		}

		// Delete user and their home directory
		script.WriteString(fmt.Sprintf("echo '%s' | sudo -S userdel -r %s 2>/dev/null || echo 'User %s not found or already deleted'\n",
			server.RootPassword, username, username))
		deleted = append(deleted, username)
	}

	if len(deleted) == 0 {
		logBuilder.WriteString("⚠️ No valid user entries found.\n")
	} else {
		logBuilder.WriteString(fmt.Sprintf("🗑️ Deleting %d users from server %s\n\n", len(deleted), ip))
		logBuilder.WriteString("Users being deleted:\n")
		for _, username := range deleted {
			logBuilder.WriteString(fmt.Sprintf("- %s\n", username))
		}
		logBuilder.WriteString("\nExecution Log:\n")
	}

	output, err := runRemoteCommand(ip, server.RootUsername, server.RootPassword, script.String())
	if err != nil {
		logBuilder.WriteString(fmt.Sprintf("❌ Remote script execution failed: %v\n", err))
	}
	logBuilder.WriteString(output)

	// Remove deleted users from accounts list
	var updatedAccounts []UserAccount
	for _, account := range server.Accounts {
		found := false
		for _, deletedUser := range deleted {
			if account.Username == deletedUser {
				found = true
				break
			}
		}
		if !found {
			updatedAccounts = append(updatedAccounts, account)
		}
	}

	server.Accounts = updatedAccounts
	ipMap[ip] = server
	saveIPMap()

	tmpl := template.Must(template.ParseFiles("templates/logs.html"))
	tmpl.Execute(w, logBuilder.String())
}
