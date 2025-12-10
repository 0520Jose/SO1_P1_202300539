package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Constantes de configuración
const SCRIPT_PATH = "../bash/generar_contenedores.sh"
const PROCS_FILE = "/proc/sysinfo_so1_202300539"
const CONT_FILE = "/proc/continfo_so1_202300539"
const DB_PATH = "./metrics.db"

// Límites de contenedores
const MAX_CONTAINERS = 10
const MIN_CONTAINERS = 5


// Estructuras de datos
type Process struct {
	Pid        int    `json:"pid"`
	Name       string `json:"name"`
	State      int    `json:"state"`
	Rss        int64  `json:"rss"`
	Vsz        int64  `json:"vsz"`
	Cpu        int64  `json:"cpu"`
	MemPercent int64  `json:"mem_percent"`
}

type SysInfo struct {
	TotalRam  int64     `json:"total_ram"`
	FreeRam   int64     `json:"free_ram"`
	UsedRam   int64     `json:"used_ram"`
	Processes []Process `json:"processes"`
}

type ContainerInfo struct {
	ID       string
	Image    string
	Name     string
	RamUsage int64
	CpuUsage int64
	IsLow    bool
}


// Función principal
func main() {
	//  Inicializacion de base de datos
	initDB()
	// Inicializacion de modulos del kernel
	loadCmd := exec.Command("bash", "../bash/load_modules.sh")
	loadCmd.Stdout = os.Stdout
	loadCmd.Stderr = os.Stderr
	if err := loadCmd.Run(); err != nil {
		log.Printf("Error: %v", err)
	}

	// Iniciar Grafana
	cmd := exec.Command("docker", "compose", "-f", "../dashboard/docker-compose.yml", "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("Error Grafana: %v", err)
	} else {
		log.Println("Grafana iniciado correctamente")
	}
	setupCronjob()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("\n\nSaliendo")
		cleanup()
		os.Exit(0)
	}()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	deleted := manageContainers()
	readAndSaveMetrics(deleted)

	// Ciclo de monitoreo
	for range ticker.C {
		deleted := manageContainers()
		readAndSaveMetrics(deleted)
	}
}

// Función de limpieza al salir
func cleanup() {
	// Eliminar cronjob
	removeCronjob()
	time.Sleep(2 * time.Second)
	checkCmd := exec.Command("bash", "-c", "crontab -l 2>/dev/null | grep generar_contenedores")
	if output, _ := checkCmd.Output(); len(output) > 0 {
		exec.Command("bash", "-c", "crontab -r").Run()
	} else {
		log.Println("Cronjob eliminado correctamente")
	}

	// Detener y eliminar contenedores Docker
	cmd := exec.Command("docker", "ps", "-a", "--format", "{{.ID}}|{{.Names}}")
	output, _ := cmd.Output()
	lines := strings.Split(string(output), "\n")
	
	stoppedCount := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		id := parts[0]
		name := parts[1]
		if strings.Contains(name, "grafana") {
			continue
		}
		
		exec.Command("docker", "stop", id).Run()
		exec.Command("docker", "rm", id).Run()
		stoppedCount++
	}
	exec.Command("sudo", "rmmod", "continfo").Run()
	exec.Command("sudo", "rmmod", "sysinfo").Run()

	// Detener Grafana
	exec.Command("docker", "compose", "-f", "../dashboard/docker-compose.yml", "down").Run()
}

// Funcion de inicialización de la base de datos
func initDB() {
	// Crear base de datos SQLite si no existe
	db, err := sql.Open("sqlite3", DB_PATH)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Crear tablas si no existen
	sqlStmt := `
	CREATE TABLE IF NOT EXISTS metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		total_ram INTEGER,
		free_ram INTEGER,
		used_ram INTEGER,
		container_count INTEGER,
		process_count INTEGER,
		deleted_count INTEGER
	);`
	db.Exec(sqlStmt)

	// Tabla para estadísticas de contenedores
	sqlStmt2 := `
	CREATE TABLE IF NOT EXISTS container_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		container_id TEXT,
		container_name TEXT,
		pid INTEGER,
		process_name TEXT,
		ram_usage INTEGER,
		cpu_usage INTEGER
	);`
	db.Exec(sqlStmt2)

	// Tabla para estadísticas de procesos
	sqlStmt3 := `
	CREATE TABLE IF NOT EXISTS process_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER NOT NULL,
		pid INTEGER,
		name TEXT,
		ram_usage INTEGER,
		cpu_usage INTEGER
	);`
	db.Exec(sqlStmt3)
}

// Función para leer metricas y guardarlas en la base de datos
func readAndSaveMetrics(deletedCount int) {
	// Leer sysinfo desde /proc
	data, err := ioutil.ReadFile(PROCS_FILE)
	if err != nil {
		log.Printf("Error leyendo /proc/sysinfo: %v", err)
		return
	}

	var info SysInfo
	if err := json.Unmarshal(data, &info); err != nil {
		log.Printf("Error parseando sysinfo: %v", err)
		return
	}

	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}|{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Error obteniendo contenedores Docker: %v", err)
		return
	}

	lines := strings.Split(string(output), "\n")
	type DockerContainer struct {
		ID   string
		Name string
	}

	// Mapa de contenedores Docker
	dockerContainers := make(map[string]DockerContainer)
	realContainerCount := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) >= 2 {
			id := parts[0]
			name := parts[1]
			if strings.Contains(name, "grafana") || strings.Contains(strings.ToLower(name), "grafana") {
				continue
			}
			dockerContainers[name] = DockerContainer{
				ID:   id,
				Name: name,
			}
			realContainerCount++
		}
	}

	db, err := sql.Open("sqlite3", DB_PATH)
	if err != nil {
		log.Printf("Error BD: %v", err)
		return
	}
	defer db.Close()

	timestamp := time.Now().Unix() * 1000

	stmt, err := db.Prepare("INSERT INTO metrics(timestamp, total_ram, free_ram, used_ram, container_count, process_count, deleted_count) values(?,?,?,?,?,?,?)")
	if err != nil {
		log.Printf("Error metrics: %v", err)
		return
	}
	_, err = stmt.Exec(timestamp, info.TotalRam, info.FreeRam, info.UsedRam, realContainerCount, len(info.Processes), deletedCount)
	if err != nil {
		log.Printf("Error insertando metrics: %v", err)
	}
	stmt.Close()

	// Leer continfo desde /proc
	contData, err := ioutil.ReadFile(CONT_FILE)
	if err != nil {
		log.Printf("Error leyendo /proc/continfo: %v", err)
	} else {
		var contProcs []Process
		if err := json.Unmarshal(contData, &contProcs); err != nil {
			log.Printf("Error parseando continfo: %v", err)
		} else {
			stmt2, err := db.Prepare("INSERT INTO container_stats(timestamp, container_id, container_name, pid, process_name, ram_usage, cpu_usage) values(?,?,?,?,?,?,?)")
			if err != nil {
				log.Printf("Error container_stats: %v", err)
			} else {
				insertedCount := 0
				
				for _, proc := range contProcs {
					foundContainer := false
					for dockerName, container := range dockerContainers {
						if strings.Contains(proc.Name, "stress") || 
						   strings.Contains(proc.Name, "sleep") ||
						   strings.Contains(proc.Name, "sh") ||
						   strings.Contains(dockerName, "so1_contenedor") {
							
							_, err = stmt2.Exec(
								timestamp, 
								container.ID[:12], 
								container.Name,
								proc.Pid, 
								proc.Name, 
								proc.Rss/1024, 
								proc.Cpu,
							)
							if err == nil {
								insertedCount++
							}
							foundContainer = true
							break
						}
					}
					
					if !foundContainer && (strings.Contains(proc.Name, "stress") || strings.Contains(proc.Name, "sleep")) {
						stmt2.Exec(
							timestamp,
							fmt.Sprintf("cont_%d", proc.Pid),
							"unknown",
							proc.Pid,
							proc.Name,
							proc.Rss/1024,
							proc.Cpu,
						)
						insertedCount++
					}
				}
				stmt2.Close()
			}
		}
	}

	stmt3, err := db.Prepare("INSERT INTO process_stats(timestamp, pid, name, ram_usage, cpu_usage) values(?,?,?,?,?)")
	if err != nil {
		log.Printf("Error  process_stats: %v", err)
	} else {
		for _, p := range info.Processes {
			stmt3.Exec(timestamp, p.Pid, p.Name, p.Rss/1024, p.Cpu)
		}
		stmt3.Close()
	}
}

// Función para gestionar contenedores Docker
func manageContainers() int {
	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}|{{.Image}}|{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Error ejecutando docker ps: %v", err)
		return 0
	}

	lines := strings.Split(string(output), "\n")

	var allContainers []ContainerInfo

	contData, _ := ioutil.ReadFile(CONT_FILE)
	var contProcs []Process
	json.Unmarshal(contData, &contProcs)

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}

		id := parts[0]
		image := parts[1]
		name := parts[2]

		if strings.Contains(name, "grafana") || strings.Contains(image, "grafana") {
			continue
		}

		var ramUsage, cpuUsage int64
		for _, proc := range contProcs {
			if strings.Contains(proc.Name, "stress") || 
			   strings.Contains(proc.Name, "sleep") ||
			   strings.Contains(proc.Name, "sh") {
				ramUsage = proc.Rss
				cpuUsage = proc.Cpu
				break
			}
		}

		container := ContainerInfo{
			ID:       id,
			Image:    image,
			Name:     name,
			RamUsage: ramUsage,
			CpuUsage: cpuUsage,
			IsLow:    strings.Contains(image, "alpine"),
		}

		allContainers = append(allContainers, container)
	}

	var lowContainers, highContainers []ContainerInfo
	for _, c := range allContainers {
		if c.IsLow {
			lowContainers = append(lowContainers, c)
		} else {
			highContainers = append(highContainers, c)
		}
	}

	totalCurrent := len(allContainers)

	if totalCurrent > MAX_CONTAINERS {
		return emergencyCleanup(lowContainers, highContainers)
	}

	sort.Slice(lowContainers, func(i, j int) bool {
		return lowContainers[i].RamUsage > lowContainers[j].RamUsage
	})
	sort.Slice(highContainers, func(i, j int) bool {
		return highContainers[i].RamUsage > highContainers[j].RamUsage
	})

	totalDeleted := 0

	if len(lowContainers) > 3 {

		for i := 3; i < len(lowContainers); i++ {
			log.Printf("   └─ [%s] RAM=%d MB CPU=%d%%", 
				lowContainers[i].Name[:min(12, len(lowContainers[i].Name))], 
				lowContainers[i].RamUsage/(1024), 
				lowContainers[i].CpuUsage)

			exec.Command("docker", "stop", lowContainers[i].ID).Run()
			exec.Command("docker", "rm", lowContainers[i].ID).Run()
			totalDeleted++
		}
	}

	if len(highContainers) > 2 {
		for i := 2; i < len(highContainers); i++ {
			log.Printf("   └─ [%s] RAM=%d MB CPU=%d%%", 
				highContainers[i].Name[:min(12, len(highContainers[i].Name))], 
				highContainers[i].RamUsage/(1024), 
				highContainers[i].CpuUsage)

			exec.Command("docker", "stop", highContainers[i].ID).Run()
			exec.Command("docker", "rm", highContainers[i].ID).Run()
			totalDeleted++
		}
	}
	return totalDeleted
}

// Función de limpieza de emergencia
func emergencyCleanup(lowContainers, highContainers []ContainerInfo) int {
	sort.Slice(lowContainers, func(i, j int) bool {
		return lowContainers[i].RamUsage > lowContainers[j].RamUsage
	})
	sort.Slice(highContainers, func(i, j int) bool {
		return highContainers[i].RamUsage > highContainers[j].RamUsage
	})

	totalDeleted := 0

	for i := 3; i < len(lowContainers); i++ {
		exec.Command("docker", "stop", lowContainers[i].ID).Run()
		exec.Command("docker", "rm", lowContainers[i].ID).Run()
		totalDeleted++
	}

	for i := 2; i < len(highContainers); i++ {
		exec.Command("docker", "stop", highContainers[i].ID).Run()
		exec.Command("docker", "rm", highContainers[i].ID).Run()
		totalDeleted++
	}
	return totalDeleted
}

// Funciones para gestionar cronjob
func setupCronjob() {
	scriptPath, err := filepath.Abs(SCRIPT_PATH)
	if err != nil {
		return
	}

	// Asegurarse de que el script es ejecutable
	exec.Command("chmod", "+x", scriptPath).Run()

	checkCmd := exec.Command("bash", "-c", "crontab -l 2>/dev/null | grep -F '"+scriptPath+"'")
	output, _ := checkCmd.Output()

	if len(output) > 0 {
		return
	}

	logPath := filepath.Join(filepath.Dir(scriptPath), "execution.log")
	cronEntry := fmt.Sprintf("* * * * * %s >> %s 2>&1", scriptPath, logPath)

	cmd := exec.Command("bash", "-c", fmt.Sprintf("(crontab -l 2>/dev/null; echo '%s') | crontab -", cronEntry))
	if err := cmd.Run(); err != nil {
		log.Printf("Error configurando cronjob: %v", err)
	} else {
		log.Println("Cronjob configurado exitosamente")
	}
}

// Función para eliminar cronjob
func removeCronjob() {
	scriptPath, err := filepath.Abs(SCRIPT_PATH)
	if err != nil {
		log.Printf("Error obteniendo ruta: %v", err)
		return
	}

	cmd := exec.Command("bash", "-c", fmt.Sprintf("crontab -l 2>/dev/null | grep -v -F '%s' | crontab -", scriptPath))
	if err := cmd.Run(); err != nil {
		log.Printf("Error al eliminar cronjob: %v", err)
	} else {
		log.Println("Cronjob eliminado del sistema")
	}
}

// Función auxiliar para obtener el mínimo de dos enteros
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}