# Manual Técnico - Proyecto 1
## Sistema de Monitoreo de Procesos y Contenedores en Linux

**Universidad San Carlos de Guatemala**  
**Facultad de Ingeniería**  
**Ingeniería en Ciencias y Sistemas**  
**Sistemas Operativos 1**  
**Carnet:** 202300539  
**Nombre:** Jose Emanuel Monzon Lemus           
**Repositorio:** https://github.com/0520Jose/SO1_P1_202300539

---

## Tabla de Contenidos

1. [Introducción](#introducción)
2. [Arquitectura del Sistema](#arquitectura-del-sistema)
3. [Módulos de Kernel](#módulos-de-kernel)
4. [Daemon en Go](#daemon-en-go)
5. [Scripts de Automatización](#scripts-de-automatización)
6. [Base de Datos SQLite](#base-de-datos-sqlite)
7. [Dashboards en Grafana](#dashboards-en-grafana)
8. [Compilación e Instalación](#compilación-e-instalación)
9. [Decisiones de Diseño](#decisiones-de-diseño)
10. [Problemas Encontrados y Soluciones](#problemas-encontrados-y-soluciones)

---

## Introducción

### Objetivo del Proyecto

Desarrollar un sistema integral de monitoreo que combina módulos de kernel en C con un daemon en Go para la gestión automatizada de contenedores Docker, con visualización en tiempo real mediante Grafana.

### Componentes Principales

1. **Dos módulos de kernel en C:**
   - `sysinfo.ko`: Monitorea procesos del sistema operativo
   - `continfo.ko`: Monitorea procesos de contenedores Docker

2. **Daemon en Go:**
   - Gestión automatizada de contenedores
   - Análisis de métricas en tiempo real
   - Almacenamiento en SQLite

3. **Sistema de automatización:**
   - Cronjob para generación de contenedores
   - Scripts Bash para carga de módulos

4. **Visualización:**
   - Dos dashboards en Grafana
   - Actualización en tiempo real

---

## Arquitectura del Sistema

### Diagrama de Componentes

```
┌─────────────────────────────────────────────────────────────┐
│                     ESPACIO DE USUARIO                       │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐      ┌──────────────┐    ┌─────────────┐ │
│  │   Grafana    │◄─────│   SQLite     │◄───│  Daemon Go  │ │
│  │  Dashboard   │      │  Database    │    │             │ │
│  └──────────────┘      └──────────────┘    └──────┬──────┘ │
│                                                     │        │
│                                                     │        │
│  ┌──────────────┐      ┌──────────────┐           │        │
│  │   Cronjob    │─────►│    Docker    │           │        │
│  │   Script     │      │  Containers  │           │        │
│  └──────────────┘      └──────────────┘           │        │
│                                                     │        │
├─────────────────────────────────────────────────────┼────────┤
│                     INTERFACE /proc                 │        │
├─────────────────────────────────────────────────────┼────────┤
│  /proc/sysinfo_so1_202300539  ◄─────────────────────┤        │
│  /proc/continfo_so1_202300539 ◄─────────────────────┘        │
├─────────────────────────────────────────────────────────────┤
│                     ESPACIO DE KERNEL                        │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐         ┌─────────────────┐           │
│  │  sysinfo.ko     │         │  continfo.ko    │           │
│  │  (Módulo Kernel)│         │  (Módulo Kernel)│           │
│  └─────────────────┘         └─────────────────┘           │
└─────────────────────────────────────────────────────────────┘
```

### Flujo de Datos

1. **Captura:** Módulos de kernel capturan métricas de procesos
2. **Exposición:** Datos expuestos en `/proc` en formato JSON
3. **Lectura:** Daemon Go lee datos cada 20 segundos
4. **Análisis:** Daemon analiza y toma decisiones de gestión
5. **Almacenamiento:** Datos guardados en SQLite
6. **Visualización:** Grafana consulta SQLite y muestra dashboards

---

## Módulos de Kernel

### 1. Módulo sysinfo.ko

#### Propósito
Monitorear todos los procesos del sistema operativo, capturando métricas de rendimiento y uso de recursos.

#### Estructura del Código

**Archivo:** `modulo-kernel/sysinfo.c`

```c
#include <linux/module.h>
#include <linux/kernel.h>
#include <linux/init.h>
#include <linux/proc_fs.h>
#include <linux/seq_file.h>
#include <linux/mm.h>
#include <linux/sched.h>
#include <linux/sched/signal.h>
#include <linux/time.h>

MODULE_LICENSE("GPL");
MODULE_AUTHOR("Emanuel");
MODULE_DESCRIPTION("Modulo de Informacion del Sistema SO1");
```

#### Funciones Principales

##### `show_sysinfo()`
Recorre todos los procesos del sistema usando la macro `for_each_process()`.

**Datos capturados:**
- PID del proceso
- Nombre del proceso (`task->comm`)
- Estado del proceso (`task->__state`)
- RSS (Resident Set Size) - memoria física en KB
- VSZ (Virtual Size) - memoria virtual en KB
- Porcentaje de CPU utilizado
- Porcentaje de memoria utilizada

**Cálculo de CPU:**
```c
cpu_time_ns = task->utime + task->stime;
elapsed_time_ns = now_ns - task->start_time;
cpu_usage_percent = div64_u64(cpu_time_ns * 100, elapsed_time_ns);
```

**Cálculo de Memoria:**
```c
rss = get_mm_rss(task->mm) << PAGE_SHIFT;
mem_usage_percent = ((rss / 1024) * 100) / total_ram;
```

##### `sysinfo_init()`
Inicializa el módulo y crea la entrada en `/proc/sysinfo_so1_202300539`.

##### `sysinfo_exit()`
Limpia y elimina la entrada de `/proc` al descargar el módulo.

#### Formato de Salida

JSON estructurado:
```json
{
  "total_ram": 8192000,
  "free_ram": 4096000,
  "used_ram": 4096000,
  "processes": [
    {
      "pid": 1234,
      "name": "bash",
      "state": 0,
      "rss": 2048,
      "mem_percent": 1,
      "vsz": 4096,
      "cpu": 5
    }
  ]
}
```

---

### 2. Módulo continfo.ko

#### Propósito
Monitorear específicamente los procesos que pertenecen a contenedores Docker, filtrando por namespace.

#### Diferencias con sysinfo

**Filtrado por Namespace:**
```c
bool is_container = false;
if (task->nsproxy && init_task_ptr->nsproxy) {
    if (task->nsproxy->uts_ns != init_task_ptr->nsproxy->uts_ns) {
        is_container = true;
    }
}
```

Este código verifica si el proceso está en un namespace diferente al del sistema, lo cual indica que pertenece a un contenedor.

#### Estructura de Datos

**Formato de salida:**
```json
[
  {
    "pid": 5678,
    "name": "stress",
    "rss": 128000,
    "mem_percent": 2,
    "vsz": 256000,
    "cpu": 85
  }
]
```

**Nota:** No incluye `state` porque solo se usa para procesos del sistema.

---

### Compilación de Módulos

#### Makefile

**Archivo:** `modulo-kernel/Makefile`

```makefile
obj-m += sysinfo.o
obj-m += continfo.o

all:
	make -C /lib/modules/$(shell uname -r)/build M=$(PWD) modules

clean:
	make -C /lib/modules/$(shell uname -r)/build M=$(PWD) clean
```

#### Comandos de Compilación

```bash
# Compilar módulos
cd modulo-kernel
make

# Verificar archivos generados
ls -lh *.ko
# Salida esperada:
# sysinfo.ko
# continfo.ko
```

#### Carga y Descarga Manual

```bash
# Cargar módulos
sudo insmod sysinfo.ko
sudo insmod continfo.ko

# Verificar carga
lsmod | grep info

# Ver mensajes del kernel
dmesg | tail -n 20

# Descargar módulos
sudo rmmod continfo
sudo rmmod sysinfo
```

#### Verificación de Funcionamiento

```bash
# Leer información del sistema
cat /proc/sysinfo_so1_202300539 | head -50

# Leer información de contenedores
cat /proc/continfo_so1_202300539

# Verificar formato JSON
cat /proc/sysinfo_so1_202300539 | python3 -m json.tool
```

---

## Daemon en Go

### Estructura del Proyecto

**Archivo:** `go-daemon/main.go`

#### Dependencias

**Archivo:** `go-daemon/go.mod`

```go
module github.com/0520Jose/SO1_P1_202300539

go 1.24.0

require (
	github.com/mattn/go-sqlite3 v1.14.32
	golang.org/x/sys v0.38.0
)
```

### Constantes de Configuración

```go
const SCRIPT_PATH = "../bash/generar_contenedores.sh"
const PROCS_FILE = "/proc/sysinfo_so1_202300539"
const CONT_FILE = "/proc/continfo_so1_202300539"
const DB_PATH = "./metrics.db"

const MAX_CONTAINERS = 10  // Límite máximo de contenedores
const MIN_CONTAINERS = 5   // Objetivo de contenedores (3 low + 2 high)
```

### Estructuras de Datos

```go
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
	IsLow    bool  // true si es alpine (bajo consumo)
}
```

### Funciones Principales

#### 1. `main()`

Función principal que orquesta todo el sistema.

**Flujo de ejecución:**

```go
func main() {
	// 1. Inicializar base de datos
	initDB()
	
	// 2. Cargar módulos del kernel
	loadCmd := exec.Command("bash", "../bash/load_modules.sh")
	loadCmd.Run()
	
	// 3. Iniciar Grafana
	exec.Command("docker", "compose", "-f", 
		"../dashboard/docker-compose.yml", "up", "-d").Run()
	
	// 4. Configurar cronjob
	setupCronjob()
	
	// 5. Capturar señales de interrupción
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cleanup()
		os.Exit(0)
	}()
	
	// 6. Loop principal cada 20 segundos
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		deleted := manageContainers()
		readAndSaveMetrics(deleted)
	}
}
```

#### 2. `initDB()`

Crea la base de datos SQLite y sus tablas.

**Tablas creadas:**

1. **metrics:** Métricas generales del sistema
```sql
CREATE TABLE metrics (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,
	total_ram INTEGER,
	free_ram INTEGER,
	used_ram INTEGER,
	container_count INTEGER,
	process_count INTEGER,
	deleted_count INTEGER
);
```

2. **container_stats:** Estadísticas de contenedores
```sql
CREATE TABLE container_stats (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,
	container_id TEXT,
	container_name TEXT,
	pid INTEGER,
	process_name TEXT,
	ram_usage INTEGER,
	cpu_usage INTEGER
);
```

3. **process_stats:** Estadísticas de procesos
```sql
CREATE TABLE process_stats (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,
	pid INTEGER,
	name TEXT,
	ram_usage INTEGER,
	cpu_usage INTEGER
);
```

**Formato de timestamp:** Unix epoch en milisegundos para compatibilidad con Grafana.

#### 3. `readAndSaveMetrics()`

Lee datos de `/proc` y los almacena en SQLite.

**Proceso:**

1. **Leer sysinfo:**
```go
data, err := ioutil.ReadFile(PROCS_FILE)
var info SysInfo
json.Unmarshal(data, &info)
```

2. **Obtener lista de contenedores Docker:**
```go
cmd := exec.Command("docker", "ps", "--format", "{{.Names}}")
output, _ := cmd.Output()
```

3. **Leer continfo:**
```go
contData, _ := ioutil.ReadFile(CONT_FILE)
var contProcs []Process
json.Unmarshal(contData, &contProcs)
```

4. **Mapear procesos a contenedores:**
```go
containerIndex := 0
for _, proc := range contProcs {
	if strings.Contains(proc.Name, "stress") || 
	   strings.Contains(proc.Name, "sleep") {
		containerName := containerNames[containerIndex]
		// Guardar en BD
		containerIndex++
	}
}
```

**Decisión de diseño:** Se usa un mapeo secuencial simple porque los PIDs del kernel corresponden a procesos hijos (stress/sleep), no al proceso principal del contenedor.

#### 4. `manageContainers()`

Gestiona los contenedores aplicando las restricciones del proyecto.

**Restricciones:**
- Máximo 10 contenedores totales
- Mantener 3 contenedores de bajo consumo (alpine)
- Mantener 2 contenedores de alto consumo (stress)
- Nunca eliminar Grafana

**Algoritmo:**

```go
func manageContainers() int {
	// 1. Obtener lista de contenedores
	cmd := exec.Command("docker", "ps", "--format", 
		"{{.ID}}|{{.Image}}|{{.Names}}")
	
	// 2. Leer métricas del kernel
	contData, _ := ioutil.ReadFile(CONT_FILE)
	var contProcs []Process
	json.Unmarshal(contData, &contProcs)
	
	// 3. Clasificar por tipo (low/high)
	for _, container := range allContainers {
		if strings.Contains(container.Image, "alpine") {
			lowContainers = append(lowContainers, container)
		} else {
			highContainers = append(highContainers, container)
		}
	}
	
	// 4. Verificar límite total
	if totalCurrent > MAX_CONTAINERS {
		return emergencyCleanup()
	}
	
	// 5. Ordenar por RAM (peor rendimiento primero)
	sort.Slice(lowContainers, func(i, j int) bool {
		return lowContainers[i].RamUsage > lowContainers[j].RamUsage
	})
	
	// 6. Eliminar exceso manteniendo los mejores
	if len(lowContainers) > 3 {
		for i := 3; i < len(lowContainers); i++ {
			exec.Command("docker", "stop", lowContainers[i].ID).Run()
			exec.Command("docker", "rm", lowContainers[i].ID).Run()
			totalDeleted++
		}
	}
	
	return totalDeleted
}
```

**Criterio de ordenamiento:** Los contenedores se ordenan por RAM de mayor a menor. Los primeros en la lista (mayor consumo) son los primeros en eliminarse, manteniendo los de mejor rendimiento.

#### 5. `setupCronjob()` y `removeCronjob()`

Gestiona el cronjob del sistema operativo.

```go
func setupCronjob() {
	scriptPath, _ := filepath.Abs(SCRIPT_PATH)
	exec.Command("chmod", "+x", scriptPath).Run()
	
	// Verificar si ya existe
	checkCmd := exec.Command("bash", "-c", 
		"crontab -l 2>/dev/null | grep -F '"+scriptPath+"'")
	output, _ := checkCmd.Output()
	
	if len(output) > 0 {
		return  // Ya existe
	}
	
	// Crear entrada
	logPath := filepath.Join(filepath.Dir(scriptPath), "execution.log")
	cronEntry := fmt.Sprintf("* * * * * %s >> %s 2>&1", 
		scriptPath, logPath)
	
	cmd := exec.Command("bash", "-c", 
		fmt.Sprintf("(crontab -l 2>/dev/null; echo '%s') | crontab -", 
		cronEntry))
	cmd.Run()
}
```

**Formato del cronjob:**
```
* * * * * /ruta/absoluta/generar_contenedores.sh >> execution.log 2>&1
```

Esto ejecuta el script cada minuto y redirige la salida al log.

#### 6. `cleanup()`

Limpia todos los recursos al detener el daemon.

**Proceso de limpieza:**

1. Eliminar cronjob del sistema
2. Verificar eliminación
3. Detener y eliminar todos los contenedores (excepto Grafana)
4. Descargar módulos del kernel
5. Detener Grafana

```go
func cleanup() {
	removeCronjob()
	
	// Verificar eliminación del cronjob
	time.Sleep(2 * time.Second)
	checkCmd := exec.Command("bash", "-c", 
		"crontab -l 2>/dev/null | grep generar_contenedores")
	if output, _ := checkCmd.Output(); len(output) > 0 {
		exec.Command("bash", "-c", "crontab -r").Run()
	}
	
	// Detener contenedores
	// ... (código de limpieza)
	
	// Descargar módulos
	exec.Command("sudo", "rmmod", "continfo").Run()
	exec.Command("sudo", "rmmod", "sysinfo").Run()
	
	// Detener Grafana
	exec.Command("docker", "compose", "-f", 
		"../dashboard/docker-compose.yml", "down").Run()
}
```

### Compilación del Daemon

```bash
# Entrar al directorio
cd go-daemon

# Descargar dependencias
go mod download

# Compilar
go build -o daemon main.go

# Verificar binario
ls -lh daemon
```

---

## Scripts de Automatización

### 1. Script load_modules.sh

**Propósito:** Compilar y cargar los módulos del kernel automáticamente.

**Archivo:** `bash/load_modules.sh`

```bash
#!/bin/bash

echo "========================================="
echo "Cargando Módulos del Kernel - SO1"
echo "========================================="

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KERNEL_DIR="$SCRIPT_DIR/../modulo-kernel"

cd "$KERNEL_DIR" || exit 1

# Limpiar módulos anteriores
echo "🧹 Limpiando módulos anteriores..."
sudo rmmod continfo 2>/dev/null
sudo rmmod sysinfo 2>/dev/null

# Limpiar compilación
echo "🔨 Limpiando compilación anterior..."
make clean > /dev/null 2>&1

# Compilar
echo "🔧 Compilando módulos..."
if make; then
    echo "✅ Compilación exitosa"
else
    echo "❌ Error en compilación"
    exit 1
fi

# Cargar módulo sysinfo
echo "📥 Cargando módulo sysinfo..."
if sudo insmod sysinfo.ko; then
    echo "✅ Módulo sysinfo cargado"
else
    echo "❌ Error cargando sysinfo"
    exit 1
fi

# Cargar módulo continfo
echo "📥 Cargando módulo continfo..."
if sudo insmod continfo.ko; then
    echo "✅ Módulo continfo cargado"
else
    echo "❌ Error cargando continfo"
    sudo rmmod sysinfo
    exit 1
fi

# Verificar
echo ""
echo "🔍 Verificando módulos cargados:"
if lsmod | grep -q "sysinfo" && lsmod | grep -q "continfo"; then
    echo "✅ Ambos módulos están activos"
    lsmod | grep "info"
else
    echo "❌ Error: Los módulos no están activos"
    exit 1
fi

# Verificar archivos /proc
echo ""
echo "🔍 Verificando archivos en /proc:"
if [ -e "/proc/sysinfo_so1_202300539" ] && 
   [ -e "/proc/continfo_so1_202300539" ]; then
    echo "✅ Archivos /proc creados correctamente"
    ls -lh /proc/sysinfo_so1_202300539 /proc/continfo_so1_202300539
else
    echo "❌ Error: Archivos /proc no encontrados"
    exit 1
fi

echo ""
echo "========================================="
echo "✅ Módulos cargados exitosamente"
echo "========================================="
```

**Dar permisos:**
```bash
chmod +x bash/load_modules.sh
```

### 2. Script generar_contenedores.sh

**Propósito:** Generar 10 contenedores aleatorios cada minuto.

**Archivo:** `bash/generar_contenedores.sh`

```bash
#!/bin/bash

IMG_LOW="alpine:latest"
IMG_HIGH="polinux/stress"

LOG_FILE="$(dirname "$0")/execution.log"

# Descargar imágenes
docker pull $IMG_LOW > /dev/null 2>&1
docker pull $IMG_HIGH > /dev/null 2>&1

# Verificar límite actual
CURRENT_COUNT=$(docker ps --filter "name=so1_contenedor" --format "{{.ID}}" | wc -l)

echo "$(date '+%a %d %b %Y %H:%M:%S %Z'): Contenedores actuales: $CURRENT_COUNT" >> "$LOG_FILE"

# Si hay 10 o más, no crear
if [ "$CURRENT_COUNT" -ge 10 ]; then
    echo "$(date '+%a %d %b %Y %H:%M:%S %Z'): ⚠️  LÍMITE ALCANZADO ($CURRENT_COUNT contenedores). NO se crean nuevos." >> "$LOG_FILE"
    exit 0
fi

# Crear solo lo necesario para llegar a 10
TO_CREATE=$((10 - CURRENT_COUNT))

echo "$(date '+%a %d %b %Y %H:%M:%S %Z'): Creando $TO_CREATE contenedores nuevos..." >> "$LOG_FILE"

for i in $(seq 1 $TO_CREATE); do
    RANDOM_TYPE=$((1 + $RANDOM % 3))
    CONTAINER_NAME="so1_contenedor_$RANDOM"

    case $RANDOM_TYPE in
        1)
            # Bajo consumo
            docker run -d --name "$CONTAINER_NAME" "$IMG_LOW" sleep infinity > /dev/null 2>&1
            ;;
        2)
            # Alto consumo RAM
            docker run -d --name "$CONTAINER_NAME" "$IMG_HIGH" stress --vm 1 --vm-bytes 128M > /dev/null 2>&1
            ;;
        3)
            # Alto consumo CPU
            docker run -d --name "$CONTAINER_NAME" "$IMG_HIGH" stress --cpu 1 > /dev/null 2>&1
            ;;
    esac
done

FINAL_COUNT=$(docker ps --filter "name=so1_contenedor" --format "{{.ID}}" | wc -l)
echo "$(date '+%a %d %b %Y %H:%M:%S %Z'): ✅ Creación completa. Total actual: $FINAL_COUNT contenedores." >> "$LOG_FILE"
```

**Imágenes Docker utilizadas:**

1. **alpine:latest** - Contenedor de bajo consumo
   - Ejecuta: `sleep infinity`
   - Uso típico: ~10-50 MB RAM

2. **polinux/stress** - Contenedor de alto consumo
   - **Variante RAM:** `stress --vm 1 --vm-bytes 128M`
   - **Variante CPU:** `stress --cpu 1`
   - Uso típico: 128+ MB RAM, 80-100% CPU

**Lógica de prevención de saturación:**

El script verifica cuántos contenedores existen antes de crear nuevos:
- Si hay ≥10: No crea ninguno
- Si hay <10: Crea solo los necesarios para llegar a 10

Esto evita la acumulación exponencial de contenedores.

---

## Base de Datos SQLite

### Esquema de la Base de Datos

**Archivo generado:** `go-daemon/metrics.db`

#### Tabla: metrics

Almacena métricas generales del sistema en cada ciclo de monitoreo.

```sql
CREATE TABLE metrics (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,           -- Unix epoch en milisegundos
	total_ram INTEGER,                    -- RAM total en KB
	free_ram INTEGER,                     -- RAM libre en KB
	used_ram INTEGER,                     -- RAM usada en KB
	container_count INTEGER,              -- Número de contenedores activos
	process_count INTEGER,                -- Número de procesos del sistema
	deleted_count INTEGER                 -- Contenedores eliminados en este ciclo
);
```

**Ejemplo de registro:**
```
timestamp: 1733779815000
total_ram: 8192000
free_ram: 4096000
used_ram: 4096000
container_count: 5
process_count: 342
deleted_count: 5
```

#### Tabla: container_stats

Almacena estadísticas individuales de cada contenedor.

```sql
CREATE TABLE container_stats (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,
	container_id TEXT,                    -- ID corto del contenedor
	container_name TEXT,                  -- Nombre del contenedor Docker
	pid INTEGER,                          -- PID del proceso
	process_name TEXT,                    -- Nombre del proceso (stress/sleep)
	ram_usage INTEGER,                    -- RAM en MB
	cpu_usage INTEGER                     -- CPU en porcentaje
);
```

**Ejemplo de registro:**
```
timestamp: 1733779815000
container_id: "cont_5678"
container_name: "so1_contenedor_12345"
pid: 5678
process_name: "stress"
ram_usage: 128
cpu_usage: 85
```

#### Tabla: process_stats

Almacena estadísticas de todos los procesos del sistema.

```sql
CREATE TABLE process_stats (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,
	pid INTEGER,
	name TEXT,
	ram_usage INTEGER,                    -- RAM en MB
	cpu_usage INTEGER                     -- CPU en porcentaje
);
```

### Consultas SQL Útiles

```sql
-- Ver últimas métricas
SELECT 
  datetime(timestamp/1000, 'unixepoch') as time,
  container_count,
  deleted_count 
FROM metrics 
ORDER BY timestamp DESC 
LIMIT 10;

-- Top 5 contenedores por RAM
SELECT 
  container_name,
  MAX(ram_usage) as max_ram
FROM container_stats
WHERE timestamp > (strftime('%s', 'now', '-1 hour') * 1000)
GROUP BY container_name
ORDER BY max_ram DESC
LIMIT 5;

-- Top 5 procesos por CPU
SELECT 
  name,
  MAX(cpu_usage) as max_cpu
FROM process_stats
WHERE timestamp > (strftime('%s', 'now', '-1 hour') * 1000)
GROUP BY name
ORDER BY max_cpu DESC
LIMIT 5;
```

---

## Dashboards en Grafana

### Configuración de Grafana

**Archivo:** `dashboard/docker-compose.yml`

```yaml
version: '3'
services:
  grafana:
    image: grafana/grafana:latest
    container_name: grafana_so1
    ports:
      - "3000:3000"
    environment:
      - GF_INSTALL_PLUGINS=frser-sqlite-datasource
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - grafana_storage:/var/lib/grafana
      - ../go-daemon/metrics.db:/var/lib/grafana/metrics.db:ro
    user: "0:0"

volumes:
  grafana_storage:
```

**Plugins instalados:**
- `frser-sqlite-datasource`: Para conectar SQLite con Grafana

**Acceso:**
- URL: http://localhost:3000
- Usuario: admin