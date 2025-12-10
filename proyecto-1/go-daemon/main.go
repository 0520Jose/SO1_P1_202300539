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
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const SCRIPT_PATH = "../bash/generar_contenedores.sh"
const PROCS_FILE = "/proc/sysinfo_so1_202300539"
const CONT_FILE = "/proc/continfo_so1_202300539"
const DB_PATH = "./metrics.db"

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

func main() {
	initDB()
	log.Println("--- Iniciando Daemon SO1 ---")

	// Cargar módulos del kernel
	log.Println("Cargando módulos del kernel...")
	loadCmd := exec.Command("bash", "../bash/load_modules.sh")
	loadCmd.Stdout = os.Stdout
	loadCmd.Stderr = os.Stderr
	if err := loadCmd.Run(); err != nil {
		log.Printf("Nota: %v (Probablemente los módulos ya estaban cargados)", err)
	}

	log.Println("Levantando Grafana...")
	cmd := exec.Command("docker", "compose", "-f", "../dashboard/docker-compose.yml", "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("Advertencia iniciando Grafana: %v", err)
	}

	// Configurar cronjob en el sistema
	setupCronjob()

	// Esperar brevemente
	time.Sleep(2 * time.Second)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cleanup()
		os.Exit(0)
	}()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	// Ejecución inicial inmediata
	manageContainers()

	for range ticker.C {
		log.Println("\n--- Ciclo de Monitoreo ---")
		deleted := manageContainers()
		readAndSaveMetrics(deleted)
	}
}

func cleanup() {
	log.Println("\nDeteniendo Daemon y limpiando...")

	// Eliminar cronjob del sistema
	removeCronjob()

	// Detener y eliminar contenedores
	log.Println("Eliminando TODOS los contenedores...")
	exec.Command("sh", "-c", "docker stop $(docker ps -aq) 2>/dev/null").Run()
	exec.Command("sh", "-c", "docker rm $(docker ps -aq) 2>/dev/null").Run()

	// Descargar módulos del kernel
	log.Println("Descargando módulos del kernel...")
	exec.Command("sudo", "rmmod", "continfo").Run()
	exec.Command("sudo", "rmmod", "sysinfo").Run()

	log.Println("✅ Limpieza completa. Adiós.")
}

func initDB() {
	db, err := sql.Open("sqlite3", DB_PATH)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	sqlStmt := `
	CREATE TABLE IF NOT EXISTS metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		total_ram INTEGER,
		free_ram INTEGER,
		used_ram INTEGER,
		container_count INTEGER,
		process_count INTEGER,
		deleted_count INTEGER
	);
	`
	_, err = db.Exec(sqlStmt)

	sqlStmt2 := `
	CREATE TABLE IF NOT EXISTS container_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		pid INTEGER,
		name TEXT,
		ram_usage INTEGER,
		cpu_usage INTEGER
	);
	`
	_, err = db.Exec(sqlStmt2)

	sqlStmt3 := `
	CREATE TABLE IF NOT EXISTS process_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		pid INTEGER,
		name TEXT,
		ram_usage INTEGER,
		cpu_usage INTEGER
	);
	`
	_, err = db.Exec(sqlStmt3)
}

func readAndSaveMetrics(deletedCount int) {
	// Intentar leer métricas del sistema
	data, err := ioutil.ReadFile(PROCS_FILE)
	if err != nil {
		log.Printf("Error leyendo proc sysinfo: %v", err)
		return
	}
	var info SysInfo
	json.Unmarshal(data, &info)

	// Intentar leer métricas de contenedores
	contData, _ := ioutil.ReadFile(CONT_FILE)
	var contProcs []Process
	json.Unmarshal(contData, &contProcs)

	db, _ := sql.Open("sqlite3", DB_PATH)
	defer db.Close()

	stmt, _ := db.Prepare("INSERT INTO metrics(total_ram, free_ram, used_ram, container_count, process_count, deleted_count) values(?,?,?,?,?,?)")
	stmt.Exec(info.TotalRam, info.FreeRam, info.UsedRam, len(contProcs), len(info.Processes), deletedCount)

	stmt2, _ := db.Prepare("INSERT INTO container_stats(pid, name, ram_usage, cpu_usage) values(?,?,?,?)")
	for _, p := range contProcs {
		stmt2.Exec(p.Pid, p.Name, p.Rss/1024, p.Cpu)
	}

	stmt3, _ := db.Prepare("INSERT INTO process_stats(pid, name, ram_usage, cpu_usage) values(?,?,?,?)")
	for _, p := range info.Processes {
		stmt3.Exec(p.Pid, p.Name, p.Rss/1024, p.Cpu)
	}

	log.Printf("Datos guardados. Procesos Sistema: %d | Contenedores detectados por Kernel: %d", len(info.Processes), len(contProcs))
}

func manageContainers() int {
	// Obtenemos contenedores ordenados por creación (el más reciente primero)
	// docker ps por defecto ordena 'created desc'
	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}|{{.Image}}|{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Error ejecutando docker ps: %v", err)
		return 0
	}

	lines := strings.Split(string(output), "\n")

	var lowContainers []string
	var highContainers []string

	log.Println("Analizando contenedores activos...")

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

		// Ignorar Grafana
		if strings.Contains(name, "grafana") || strings.Contains(image, "grafana") {
			continue
		}

		// Clasificar (Los primeros que entran a la lista son los más nuevos)
		if strings.Contains(image, "alpine") {
			lowContainers = append(lowContainers, id)
		} else if strings.Contains(image, "stress") {
			highContainers = append(highContainers, id)
		}
	}

	totalDeleted := 0

	// Lógica Estricta: Mantener solo los 3 más nuevos de Low
	if len(lowContainers) > 3 {
		log.Printf("Exceso Low detectado (%d). Eliminando %d antiguos...", len(lowContainers), len(lowContainers)-3)
		for i := 3; i < len(lowContainers); i++ {
			exec.Command("docker", "stop", lowContainers[i]).Run()
			exec.Command("docker", "rm", lowContainers[i]).Run()
			totalDeleted++
		}
	}

	// Lógica Estricta: Mantener solo los 2 más nuevos de High
	if len(highContainers) > 2 {
		log.Printf("Exceso High detectado (%d). Eliminando %d antiguos...", len(highContainers), len(highContainers)-2)
		for i := 2; i < len(highContainers); i++ {
			exec.Command("docker", "stop", highContainers[i]).Run()
			exec.Command("docker", "rm", highContainers[i]).Run()
			totalDeleted++
		}
	}

	log.Printf("Resumen Gestión: %d Low conservados | %d High conservados | %d Eliminados",
		min(len(lowContainers), 3), min(len(highContainers), 2), totalDeleted)

	return totalDeleted
}

func killExcess(containers []string, limit int, label string) int {
	deleted := 0
	if len(containers) > limit {
		diff := len(containers) - limit
		toKill := containers[:diff]

		for _, id := range toKill {
			exec.Command("docker", "stop", id).Run()
			exec.Command("docker", "rm", id).Run()
			deleted++
		}
	}
	return deleted
}

func setupCronjob() {
	scriptPath, err := filepath.Abs(SCRIPT_PATH)
	if err != nil {
		log.Printf("Error ruta script: %v", err)
		return
	}

	exec.Command("chmod", "+x", scriptPath).Run()

	// Verificar si existe
	checkCmd := exec.Command("bash", "-c", "crontab -l 2>/dev/null | grep -F '"+scriptPath+"'")
	output, _ := checkCmd.Output()

	if len(output) > 0 {
		log.Println("Cronjob ya configurado.")
		return
	}

	// Crear
	logPath := filepath.Join(filepath.Dir(scriptPath), "execution.log")
	cronEntry := fmt.Sprintf("* * * * * %s >> %s 2>&1", scriptPath, logPath)
	cmd := exec.Command("bash", "-c", fmt.Sprintf("(crontab -l 2>/dev/null; echo '%s') | crontab -", cronEntry))
	if err := cmd.Run(); err != nil {
		log.Printf("Error al crear cronjob: %v", err)
	} else {
		log.Println("✅ Cronjob activado exitosamente.")
	}
}

func removeCronjob() {
	scriptPath, err := filepath.Abs(SCRIPT_PATH)
	if err != nil {
		return
	}
	cmd := exec.Command("bash", "-c", fmt.Sprintf("crontab -l 2>/dev/null | grep -v -F '%s' | crontab -", scriptPath))
	cmd.Run()
	log.Println("Cronjob eliminado.")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}