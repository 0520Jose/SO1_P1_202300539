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
	log.Println("Iniciando Daemon SO1...")

	// Cargar módulos del kernel
	log.Println("Cargando módulos del kernel...")
	loadCmd := exec.Command("bash", "../bash/load_modules.sh")
	loadCmd.Stdout = os.Stdout
	loadCmd.Stderr = os.Stderr
	if err := loadCmd.Run(); err != nil {
		log.Printf("Advertencia cargando módulos: %v", err)
	}

	log.Println("Levantando Grafana...")
	cmd := exec.Command("docker", "compose", "-f", "../dashboard/docker-compose.yml", "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		log.Printf("Advertencia iniciando Grafana: %v", err)
	}

	// Configurar cronjob en el sistema
	setupCronjob()

	// Esperar 5 segundos para que el cronjob comience
	time.Sleep(5 * time.Second)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cleanup()
		os.Exit(0)
	}()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("--- Ciclo de Monitoreo ---")
		deleted := manageContainers()
		readAndSaveMetrics(deleted)
	}
}

func cleanup() {
	log.Println("Deteniendo Daemon y limpiando...")
	
	// Eliminar cronjob del sistema
	removeCronjob()
	
	// Detener y eliminar contenedores
	log.Println("Eliminando todos los contenedores...")
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
	data, err := ioutil.ReadFile(PROCS_FILE)
	if err != nil {
		log.Printf("Error leyendo proc sysinfo: %v", err)
		return
	}
	var info SysInfo
	json.Unmarshal(data, &info)

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

	log.Printf("Datos guardados. Procesos Sistema: %d | Contenedores: %d | Eliminados: %d", len(info.Processes), len(contProcs), deletedCount)
}

func manageContainers() int {
	// Leer métricas de contenedores desde /proc
	contData, err := ioutil.ReadFile(CONT_FILE)
	if err != nil {
		log.Printf("Error leyendo continfo: %v", err)
		return 0
	}
	
	var contProcs []Process
	if err := json.Unmarshal(contData, &contProcs); err != nil {
		log.Printf("Error parseando continfo: %v", err)
		return 0
	}

	// Obtener lista de contenedores activos de Docker
	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}|{{.Image}}|{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Error ejecutando docker ps: %v", err)
		return 0
	}

	lines := strings.Split(string(output), "\n")
	
	type ContainerInfo struct {
		ID       string
		Image    string
		Name     string
		RamUsage int64
		CpuUsage int64
	}
	
	var lowContainers []ContainerInfo
	var highContainers []ContainerInfo

	// Mapear contenedores con sus métricas del /proc
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

		// No tocar Grafana
		if strings.Contains(name, "grafana") || strings.Contains(image, "grafana") {
			continue
		}

		// Buscar métricas correspondientes en /proc
		var ramUsage, cpuUsage int64
		for _, proc := range contProcs {
			// Intentar correlacionar por nombre del proceso
			if strings.Contains(proc.Name, "stress") || strings.Contains(proc.Name, "sleep") {
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
		}

		if strings.Contains(image, "alpine") {
			lowContainers = append(lowContainers, container)
		} else if strings.Contains(image, "stress") {
			highContainers = append(highContainers, container)
		}
	}

	// Ordenar por consumo de RAM (descendente - mayor consumo primero)
	sort.Slice(lowContainers, func(i, j int) bool {
		return lowContainers[i].RamUsage > lowContainers[j].RamUsage
	})
	sort.Slice(highContainers, func(i, j int) bool {
		return highContainers[i].RamUsage > highContainers[j].RamUsage
	})

	totalDeleted := 0

	// Eliminar exceso de contenedores de bajo consumo (mantener solo 3)
	if len(lowContainers) > 3 {
		log.Printf("⚠️  Exceso de contenedores bajo consumo: %d (límite: 3)", len(lowContainers))
		for i := 3; i < len(lowContainers); i++ {
			log.Printf("🗑️  Eliminando bajo consumo: %s (RAM: %d KB)", 
				lowContainers[i].Name, lowContainers[i].RamUsage/1024)
			exec.Command("docker", "stop", lowContainers[i].ID).Run()
			exec.Command("docker", "rm", lowContainers[i].ID).Run()
			totalDeleted++
		}
	}

	// Eliminar exceso de contenedores de alto consumo (mantener solo 2)
	if len(highContainers) > 2 {
		log.Printf("⚠️  Exceso de contenedores alto consumo: %d (límite: 2)", len(highContainers))
		for i := 2; i < len(highContainers); i++ {
			log.Printf("🗑️  Eliminando alto consumo: %s (RAM: %d KB, CPU: %d%%)", 
				highContainers[i].Name, highContainers[i].RamUsage/1024, highContainers[i].CpuUsage)
			exec.Command("docker", "stop", highContainers[i].ID).Run()
			exec.Command("docker", "rm", highContainers[i].ID).Run()
			totalDeleted++
		}
	}

	log.Printf("📊 Estado actual: %d bajo consumo | %d alto consumo | %d eliminados", 
		len(lowContainers), len(highContainers), totalDeleted)
	
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
	log.Println("Configurando cronjob en el sistema...")
	
	// Obtener ruta absoluta del script
	scriptPath, err := filepath.Abs(SCRIPT_PATH)
	if err != nil {
		log.Printf("Error obteniendo ruta del script: %v", err)
		return
	}

	// Hacer el script ejecutable
	exec.Command("chmod", "+x", scriptPath).Run()
	
	// Verificar si ya existe el cronjob
	checkCmd := exec.Command("bash", "-c", "crontab -l 2>/dev/null | grep -F '"+scriptPath+"'")
	output, _ := checkCmd.Output()
	
	if len(output) > 0 {
		log.Println("Cronjob ya existe, saltando configuración")
		return
	}
	
	// Crear entrada de cron (cada minuto)
	logPath := filepath.Join(filepath.Dir(scriptPath), "execution.log")
	cronEntry := fmt.Sprintf("* * * * * %s >> %s 2>&1", scriptPath, logPath)
	
	// Agregar a crontab
	cmd := exec.Command("bash", "-c", fmt.Sprintf("(crontab -l 2>/dev/null; echo '%s') | crontab -", cronEntry))
	if err := cmd.Run(); err != nil {
		log.Printf("Error configurando cronjob: %v", err)
	} else {
		log.Println("✅ Cronjob configurado exitosamente")
		log.Printf("Comando cron: %s", cronEntry)
	}
}

func removeCronjob() {
	log.Println("Eliminando cronjob del sistema...")
	
	scriptPath, err := filepath.Abs(SCRIPT_PATH)
	if err != nil {
		log.Printf("Error obteniendo ruta del script: %v", err)
		return
	}
	
	// Eliminar línea del crontab que contenga el script
	cmd := exec.Command("bash", "-c", fmt.Sprintf("crontab -l 2>/dev/null | grep -v -F '%s' | crontab -", scriptPath))
	if err := cmd.Run(); err != nil {
		log.Printf("Advertencia eliminando cronjob: %v", err)
	} else {
		log.Println("✅ Cronjob eliminado exitosamente")
	}
}