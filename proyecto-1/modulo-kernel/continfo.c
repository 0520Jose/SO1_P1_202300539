#include <linux/module.h>
#include <linux/kernel.h>
#include <linux/init.h>
#include <linux/proc_fs.h>
#include <linux/seq_file.h>
#include <linux/sched.h>
#include <linux/sched/signal.h>
#include <linux/mm.h>
#include <linux/nsproxy.h>
#include <linux/pid_namespace.h>
#include <linux/time.h>

MODULE_LICENSE("GPL");
MODULE_AUTHOR("Emanuel");
MODULE_DESCRIPTION("Modulo de Contenedores SO1");

static int show_continfo(struct seq_file *m, void *v) {
    struct task_struct *task;
    struct task_struct *init_task_ptr = &init_task; 
    unsigned long rss;
    bool first = true;
    u64 cpu_time_ns;
    u64 elapsed_time_ns;
    u64 cpu_usage_percent;
    u64 now_ns;

    seq_printf(m, "[\n");

    for_each_process(task) {
        
        bool is_container = false;
        if (task->nsproxy && init_task_ptr->nsproxy) {
            if (task->nsproxy->uts_ns != init_task_ptr->nsproxy->uts_ns) {
                is_container = true;
            }
        }

        if (is_container) {
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

            seq_printf(m, "  {\n");
            seq_printf(m, "    \"pid\": %d,\n", task->pid);
            seq_printf(m, "    \"name\": \"%s\",\n", task->comm);
            seq_printf(m, "    \"rss\": %lu,\n", rss / 1024); 
            seq_printf(m, "    \"vsz\": %lu,\n", (task->mm) ? (task->mm->total_vm << (PAGE_SHIFT - 10)) : 0);
            seq_printf(m, "    \"cpu\": %llu\n", cpu_usage_percent);
            seq_printf(m, "  }");
            first = false;
        }
    }

    seq_printf(m, "\n]\n");
    return 0;
}

static int continfo_open(struct inode *inode, struct file *file) {
    return single_open(file, show_continfo, NULL);
}

static const struct proc_ops continfo_ops = {
    .proc_open = continfo_open,
    .proc_read = seq_read,
    .proc_lseek = seq_lseek,
    .proc_release = single_release,
};

static int __init continfo_init(void) {
    proc_create("continfo_so1_202300539", 0, NULL, &continfo_ops);
    printk(KERN_INFO "Modulo continfo cargado.\n");
    return 0;
}

static void __exit continfo_exit(void) {
    remove_proc_entry("continfo_so1_202300539", NULL);
    printk(KERN_INFO "Modulo continfo descargado.\n");
}

module_init(continfo_init);
module_exit(continfo_exit);