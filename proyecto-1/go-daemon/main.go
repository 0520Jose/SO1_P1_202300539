package main

import (
	"database/sql"
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Ajusta esta ruta si es necesario. Asumimos que estás en /go-daemon y el script en /bash
const SCRIPT_PATH = "../bash/generar_contenedores.sh" 

const PROCS_FILE = "/proc/sysinfo_so1_202300539"
const CONT_FILE = "/proc/continfo_so1_202300539"
const DB_PATH = "./metrics.db"

type Process struct {
	Pid   int    `json:"pid"`
	Name  string `json:"name"`
	State int    `json:"state"`
	Rss   int64  `json:"rss"`
	Vsz   int64  `json:"vsz"`
	Cpu   int64  `json:"cpu"`
}

type SysInfo struct {
	TotalRam  int64     `json:"total_ram"`
	FreeRam   int64     `json:"free_ram"`
	UsedRam   int64     `json:"used_ram"`
	Processes []Process `json:"processes"`
}

func main() {
	initDB()
	log.Println("Iniciando Daemon SO1...")

	// 1. Cargar modulos
	exec.Command("sudo", "insmod", "../modulo-kernel/sysinfo.ko").Run()
	exec.Command("sudo", "insmod", "../modulo-kernel/continfo.ko").Run()

	// 2. Iniciar el "Cronjob" interno en paralelo (Goroutine)
	// Esto ejecutará el script cada 1 minuto sin detener el resto del programa
	go startGenerationService()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cleanup()
		os.Exit(0)
	}()

	// 3. Loop de monitoreo (Cada 20 segundos)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("--- Ciclo de Monitoreo ---")
		
		deleted := manageContainers()
		readAndSaveMetrics(deleted)
	}
}

// --- NUEVA FUNCION PARA EJECUTAR EL SCRIPT ---
func startGenerationService() {
	log.Println("Iniciando servicio de generacion de contenedores (cada 60s)")

	// Ejecutar inmediatamente la primera vez
	runScript()

	// Configurar el ticker para cada 1 minuto
	tickerGen := time.NewTicker(60 * time.Second)
	defer tickerGen.Stop()

	for range tickerGen.C {
		runScript()
	}
}

func runScript() {
	log.Println("Ejecutando script de generacion...")
	
	// Ejecutamos bash indicando la ruta del script
	cmd := exec.Command("/bin/bash", SCRIPT_PATH)
	
	// Capturamos salida por si hay errores en el script bash
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error ejecutando script: %v | Salida: %s", err, string(output))
	} else {
		log.Println("Contenedores generados exitosamente.")
	}
}
// ---------------------------------------------

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
}

func readAndSaveMetrics(deletedCount int) {
	data, err := ioutil.ReadFile(PROCS_FILE)
	if err != nil {
		log.Printf("Error leyendo proc: %v", err)
		return
	}

	var info SysInfo
	json.Unmarshal(data, &info)

	contData, _ := ioutil.ReadFile(CONT_FILE)
	var contProcs []Process
	json.Unmarshal(contData, &contProcs)

	db, _ := sql.Open("sqlite3", DB_PATH)
	defer db.Close()

	stmt, _ := db.Prepare("INSERT INTO metrics(total_ram, free_ram, used_ram, container_count, deleted_count) values(?,?,?,?,?)")
	stmt.Exec(info.TotalRam, info.FreeRam, info.UsedRam, len(contProcs), deletedCount)

	stmt2, _ := db.Prepare("INSERT INTO container_stats(pid, name, ram_usage, cpu_usage) values(?,?,?,?)")
	for _, p := range contProcs {
		stmt2.Exec(p.Pid, p.Name, p.Rss/1024, p.Cpu)
	}

	log.Printf("Datos guardados. Eliminados en esta ronda: %d", deletedCount)
}

func manageContainers() int {
	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}|{{.Image}}|{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Error ejecutando docker ps: %v", err)
		return 0
	}

	lines := strings.Split(string(output), "\n")
	var lowContainers []string
	var highContainers []string

	for _, line := range lines {
		if line == "" { continue }
		parts := strings.Split(line, "|")
		if len(parts) < 3 { continue }
		
		id := parts[0]
		image := parts[1]
		name := parts[2]

		if strings.Contains(name, "grafana") || strings.Contains(image, "grafana") {
			continue
		}

		if strings.Contains(image, "so1_low") {
			lowContainers = append(lowContainers, id)
		} else if strings.Contains(image, "so1_high") {
			highContainers = append(highContainers, id)
		}
	}

	totalDeleted := 0
	totalDeleted += killExcess(lowContainers, 3, "Bajo Consumo")
	totalDeleted += killExcess(highContainers, 2, "Alto Consumo")
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

func cleanup() {
	log.Println("Deteniendo Daemon y limpiando...")
	exec.Command("sudo", "rmmod", "sysinfo").Run()
	exec.Command("sudo", "rmmod", "continfo").Run()
	log.Println("Modulos descargados. Adios.")
}