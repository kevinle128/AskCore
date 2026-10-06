#define FUSE_USE_VERSION 31
#define _GNU_SOURCE
#include <fuse3/fuse.h>
#include <fuse3/fuse_lowlevel.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/file.h>
#include <sys/statvfs.h>
#include <pthread.h>
#include <poll.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

/* Test-only passthrough mount. No credential contents enter the control channel. */
static char backing[PATH_MAX];
static pthread_mutex_t mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t changed = PTHREAD_COND_INITIALIZER;
static int listener, stopping, armed, sync_count, blocked, released, file_synced;
static int directory_synced, exit_released;

static int result(int rc) { return rc < 0 ? -errno : rc; }
static int fullpath(char out[PATH_MAX], const char *path) {
    int n = snprintf(out, PATH_MAX, "%s%s", backing, path);
    return n >= PATH_MAX ? -ENAMETOOLONG : 0;
}
#define PATH_OR_RETURN(path) char p[PATH_MAX]; int e = fullpath(p, path); if (e) return e
static int gate_stopping(struct fuse *fs) {
    return stopping || (fs && fuse_session_exited(fuse_get_session(fs)));
}
static struct timespec deadline(int seconds) {
    struct timespec t;
    clock_gettime(CLOCK_REALTIME, &t);
    t.tv_sec += seconds;
    return t;
}
/* Short waits let libfuse's termination handler release blocked callbacks. */
static int wait_gate(int *condition, int seconds, struct fuse *fs) {
    struct timespec end = deadline(seconds);
    while (!*condition && !gate_stopping(fs)) {
        struct timespec tick;
        clock_gettime(CLOCK_REALTIME, &tick);
        if (tick.tv_sec > end.tv_sec || (tick.tv_sec == end.tv_sec && tick.tv_nsec >= end.tv_nsec)) return ETIMEDOUT;
        tick.tv_nsec += 100000000;
        if (tick.tv_nsec >= 1000000000) { tick.tv_sec++; tick.tv_nsec -= 1000000000; }
        pthread_cond_timedwait(&changed, &mutex, &tick);
    }
    return gate_stopping(fs) ? EINTR : 0;
}
static int get_attributes(const char *path, struct stat *st, struct fuse_file_info *fi) {
    PATH_OR_RETURN(path);
    if (!fi) return result(lstat(p, st));
    if (lstat(p, st) == 0 && S_ISDIR(st->st_mode))
        return result(fstat(dirfd((DIR *)(uintptr_t)fi->fh), st));
    return result(fstat((int)fi->fh, st));
}
static int open_file(const char *path, struct fuse_file_info *fi) {
    PATH_OR_RETURN(path);
    int fd = open(p, fi->flags);
    if (fd < 0) return -errno;
    fi->fh = (uint64_t)fd;
    fi->direct_io = 1;
    return 0;
}
static int create_file(const char *path, mode_t mode, struct fuse_file_info *fi) {
    PATH_OR_RETURN(path);
    int fd = open(p, fi->flags | O_CREAT, mode);
    if (fd < 0) return -errno;
    fi->fh = (uint64_t)fd;
    fi->direct_io = 1;
    return 0;
}
static int read_file(const char *path, char *buf, size_t size, off_t offset, struct fuse_file_info *fi) {
    (void)path;
    return result((int)pread((int)fi->fh, buf, size, offset));
}
static int write_file(const char *path, const char *buf, size_t size, off_t offset, struct fuse_file_info *fi) {
    (void)path;
    return result((int)pwrite((int)fi->fh, buf, size, offset));
}
static int release_file(const char *path, struct fuse_file_info *fi) {
    (void)path;
    return result(close((int)fi->fh));
}
static int sync_file(const char *path, int data_only, struct fuse_file_info *fi) {
    (void)path;
    struct stat st;
    if (fstat((int)fi->fh, &st) < 0) return -errno;
    int gated = 0;
    pthread_mutex_lock(&mutex);
    if (armed && S_ISREG(st.st_mode) && ++sync_count == 2) {
        gated = blocked = 1;
        pthread_cond_broadcast(&changed);
        int e = wait_gate(&released, 30, fuse_get_context()->fuse);
        if (e) { pthread_mutex_unlock(&mutex); return -e; }
    }
    pthread_mutex_unlock(&mutex);
    int rc = result(data_only ? fdatasync((int)fi->fh) : fsync((int)fi->fh));
    if (gated && rc == 0) {
        pthread_mutex_lock(&mutex);
        file_synced = 1;
        pthread_cond_broadcast(&changed);
        pthread_mutex_unlock(&mutex);
    }
    return rc;
}
static int make_directory(const char *path, mode_t mode) { PATH_OR_RETURN(path); return result(mkdir(p, mode)); }
static int remove_file(const char *path) { PATH_OR_RETURN(path); return result(unlink(p)); }
static int remove_directory(const char *path) { PATH_OR_RETURN(path); return result(rmdir(p)); }
static int rename_file(const char *from, const char *to, unsigned int flags) {
    PATH_OR_RETURN(from);
    char target[PATH_MAX];
    if ((e = fullpath(target, to))) return e;
    return result(renameat2(AT_FDCWD, p, AT_FDCWD, target, flags));
}
static int change_mode(const char *path, mode_t mode, struct fuse_file_info *fi) {
    PATH_OR_RETURN(path);
    return result(fi ? fchmod((int)fi->fh, mode) : chmod(p, mode));
}
static int change_times(const char *path, const struct timespec times[2], struct fuse_file_info *fi) {
    PATH_OR_RETURN(path);
    return result(fi ? futimens((int)fi->fh, times) : utimensat(AT_FDCWD, p, times, AT_SYMLINK_NOFOLLOW));
}
static int truncate_file(const char *path, off_t size, struct fuse_file_info *fi) {
    PATH_OR_RETURN(path);
    return result(fi ? ftruncate((int)fi->fh, size) : truncate(p, size));
}
static int flock_file(const char *path, struct fuse_file_info *fi, int operation) {
    (void)path;
    return result(flock((int)fi->fh, operation));
}
static int open_directory(const char *path, struct fuse_file_info *fi) {
    PATH_OR_RETURN(path);
    DIR *dir = opendir(p);
    if (!dir) return -errno;
    fi->fh = (uint64_t)(uintptr_t)dir;
    return 0;
}
static int read_directory(const char *path, void *buf, fuse_fill_dir_t fill, off_t offset,
                          struct fuse_file_info *fi, enum fuse_readdir_flags flags) {
    (void)path; (void)flags;
    DIR *dir = (DIR *)(uintptr_t)fi->fh;
    seekdir(dir, offset);
    struct dirent *entry;
    while ((entry = readdir(dir))) {
        struct stat st = {.st_ino = entry->d_ino, .st_mode = (mode_t)entry->d_type << 12};
        if (fill(buf, entry->d_name, &st, telldir(dir), 0)) break;
    }
    return 0;
}
static int release_directory(const char *path, struct fuse_file_info *fi) {
    (void)path;
    return result(closedir((DIR *)(uintptr_t)fi->fh));
}
static int sync_directory(const char *path, int data_only, struct fuse_file_info *fi) {
    (void)path; (void)data_only;
    int rc = result(fsync(dirfd((DIR *)(uintptr_t)fi->fh)));
    pthread_mutex_lock(&mutex);
    if (!rc && file_synced && !directory_synced) {
        directory_synced = 1;
        pthread_cond_broadcast(&changed);
        int e = wait_gate(&exit_released, 30, fuse_get_context()->fuse);
        if (e) rc = -e;
    }
    pthread_mutex_unlock(&mutex);
    return rc;
}
static int filesystem_stats(const char *path, struct statvfs *st) { PATH_OR_RETURN(path); return result(statvfs(p, st)); }
static void *initialize(struct fuse_conn_info *connection, struct fuse_config *config) {
    (void)connection;
    config->use_ino = 1;
    config->entry_timeout = config->attr_timeout = config->negative_timeout = 0;
    return NULL;
}
static const struct fuse_operations operations = {
    .init = initialize, .getattr = get_attributes, .open = open_file, .create = create_file,
    .read = read_file, .write = write_file, .release = release_file, .fsync = sync_file,
    .mkdir = make_directory, .unlink = remove_file, .rmdir = remove_directory,
    .rename = rename_file, .chmod = change_mode, .utimens = change_times, .truncate = truncate_file,
    .flock = flock_file, .opendir = open_directory, .readdir = read_directory,
    .releasedir = release_directory, .fsyncdir = sync_directory, .statfs = filesystem_stats,
};
static void *control(void *unused) {
    (void)unused;
    for (;;) {
        pthread_mutex_lock(&mutex);
        int end = stopping;
        pthread_mutex_unlock(&mutex);
        if (end) return NULL;
        struct pollfd poller = {.fd = listener, .events = POLLIN};
        if (poll(&poller, 1, 100) <= 0) continue;
        int client = accept4(listener, NULL, NULL, SOCK_CLOEXEC);
        if (client < 0) continue;
        struct timeval timeout = {.tv_sec = 2};
        setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout));
        char command[32];
        size_t length = 0;
        int complete = 0;
        while (length < sizeof(command) - 1) {
            char c;
            if (recv(client, &c, 1, 0) != 1) break;
            if (c == '\n') { complete = 1; break; }
            command[length++] = c;
        }
        command[length] = 0;
        const char *reply = "ERROR\n";
        pthread_mutex_lock(&mutex);
        if (complete && !strcmp(command, "ARM")) {
            if (!armed || exit_released) {
                armed = 1; sync_count = blocked = released = file_synced = directory_synced = exit_released = 0;
                reply = "OK\n";
            }
        } else if (complete && !strcmp(command, "WAIT")) {
            reply = wait_gate(&blocked, 20, NULL) ? "TIMEOUT\n" : "BLOCKED\n";
        } else if (complete && !strcmp(command, "WAIT_SYNC")) {
            reply = wait_gate(&directory_synced, 20, NULL) ? "TIMEOUT\n" : "SYNCED\n";
        } else if (complete && !strcmp(command, "RELEASE")) {
            released = 1; pthread_cond_broadcast(&changed); reply = "OK\n";
        } else if (complete && !strcmp(command, "RELEASE_EXIT")) {
            exit_released = 1; pthread_cond_broadcast(&changed); reply = "OK\n";
        } else if (complete && !strcmp(command, "STATUS")) {
            reply = directory_synced ? "SYNCED\n" : released ? "RELEASED\n" : blocked ? "BLOCKED\n" : armed ? "ARMED\n" : "IDLE\n";
        }
        pthread_mutex_unlock(&mutex);
        send(client, reply, strlen(reply), MSG_NOSIGNAL);
        close(client);
    }
}
int main(int argc, char **argv) {
    if (argc != 4) { fprintf(stderr, "usage: %s BACKING MOUNT SOCKET\n", argv[0]); return 2; }
    struct stat st;
    if (!realpath(argv[1], backing) || stat(backing, &st) || !S_ISDIR(st.st_mode) ||
        st.st_uid != geteuid() || (st.st_mode & 0777) != 0700) {
        fprintf(stderr, "backing must be an owned mode-0700 directory\n"); return 2;
    }
    struct sockaddr_un address = {.sun_family = AF_UNIX};
    if (strlen(argv[3]) >= sizeof(address.sun_path)) { fprintf(stderr, "socket path too long\n"); return 2; }
    strcpy(address.sun_path, argv[3]);
    umask(0077);
    listener = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (listener < 0 || bind(listener, (struct sockaddr *)&address, sizeof(address)) || listen(listener, 8)) {
        perror("control socket"); if (listener >= 0) close(listener); return 2;
    }
    pthread_t controller;
    if (pthread_create(&controller, NULL, control, NULL)) { close(listener); unlink(argv[3]); return 2; }
    char *fuse_arguments[] = {argv[0], "-f", "-o", "default_permissions", argv[2]};
    int rc = fuse_main(5, fuse_arguments, &operations, NULL);
    pthread_mutex_lock(&mutex);
    stopping = released = exit_released = 1;
    pthread_cond_broadcast(&changed);
    pthread_mutex_unlock(&mutex);
    pthread_join(controller, NULL);
    close(listener);
    unlink(argv[3]);
    return rc;
}
