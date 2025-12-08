#include <linux/module.h>
#include <linux/kernel.h>
#include <linux/init.h>
#include <linux/proc_fs.h>
#include <linux/seq_file.h>
#include <linux/mm.h>
#include <linux/sched.h>
#include <linux/timer.h>
#include <linux/jiffies.h>
#include <linux/sched/signal.h>
#include <linux/time.h>

MODULE_LICENSE("GPL");
MODULE_AUTHOR("Emanuel");
MODULE_DESCRIPTION("Modulo de Informacion del Sistema SO1");

static int show_sysinfo(struct seq_file *m, void *v) {
    struct sysinfo i;
    struct task_struct *task;
    unsigned long rss;
    long total_ram, free_ram, used_ram;
    bool first = true;
    u64 cpu_time_ns;
    u64 elapsed_time_ns;
    u64 cpu_usage_percent;
    u64 now_ns;
    u64 mem_usage_percent;

    si_meminfo(&i);

    total_ram = (i.totalram * 4); 
    free_ram = (i.freeram * 4);
    used_ram = total_ram - free_ram;

    seq_printf(m, "{\n");
    seq_printf(m, "  \"total_ram\": %ld,\n", total_ram / 1024);
    seq_printf(m, "  \"free_ram\": %ld,\n", free_ram / 1024);
    seq_printf(m, "  \"used_ram\": %ld,\n", used_ram / 1024);
    seq_printf(m, "  \"processes\": [\n");

    for_each_process(task) {
        if (!first) {
            seq_printf(m, ",\n");
        }
        
        if (task->mm) {
            rss = get_mm_rss(task->mm) << PAGE_SHIFT;
        } else {
            rss = 0;
        }

        now_ns = ktime_get_ns();
        cpu_time_ns = task->utime + task->stime;
        cpu_usage_percent = 0;
        
        if (task->start_time != 0) {
             elapsed_time_ns = now_ns - task->start_time;
             if (elapsed_time_ns > 0) {
                 cpu_usage_percent = div64_u64(cpu_time_ns * 100, elapsed_time_ns);
             }
        }

        mem_usage_percent = 0;
        if (total_ram > 0) {
            mem_usage_percent = ((rss / 1024) * 100) / total_ram;
        }

        seq_printf(m, "    {\n");
        seq_printf(m, "      \"pid\": %d,\n", task->pid);
        seq_printf(m, "      \"name\": \"%s\",\n", task->comm);
        seq_printf(m, "      \"state\": %ld,\n", task->__state);
        seq_printf(m, "      \"rss\": %lu,\n", rss / 1024);
        seq_printf(m, "      \"mem_percent\": %llu,\n", mem_usage_percent);
        seq_printf(m, "      \"vsz\": %lu,\n", (task->mm) ? (task->mm->total_vm << (PAGE_SHIFT - 10)) : 0);
        seq_printf(m, "      \"cpu\": %llu\n", cpu_usage_percent);
        seq_printf(m, "    }");
        first = false;
    }

    seq_printf(m, "\n  ]\n");
    seq_printf(m, "}\n");
    return 0;
}

static int sysinfo_open(struct inode *inode, struct file *file) {
    return single_open(file, show_sysinfo, NULL);
}

static const struct proc_ops sysinfo_ops = {
    .proc_open = sysinfo_open,
    .proc_read = seq_read,
    .proc_lseek = seq_lseek,
    .proc_release = single_release,
};

static int __init sysinfo_init(void) {
    proc_create("sysinfo_so1_202300539", 0, NULL, &sysinfo_ops);
    printk(KERN_INFO "Modulo sysinfo cargado.\n");
    return 0;
}

static void __exit sysinfo_exit(void) {
    remove_proc_entry("sysinfo_so1_202300539", NULL);
    printk(KERN_INFO "Modulo sysinfo descargado.\n");
}

module_init(sysinfo_init);
module_exit(sysinfo_exit);
