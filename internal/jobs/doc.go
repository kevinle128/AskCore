// Package jobs owns the background work of one session: the job table of the
// bash and monitor tools.
//
// A Table starts every command as a job in its own process group (through
// sandbox), waits for a job with a budget, keeps the read cursor of each job,
// refuses more than ten active jobs, and kills everything when it closes. A
// job that ends without the model having seen its end sends one completion
// notice, and a monitor sends the stdout lines of its command as events. Both
// reach the Agent through the Notifier that the composition root sets, so this
// package never imports agent.
package jobs
