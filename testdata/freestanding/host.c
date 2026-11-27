/* A stand-in kernel: no libc, no WASI. It owns guest memory and CPU state, runs
   the MFL core and services the guest's ecalls itself. */
#include "kernel.h"

int64_t run(int64_t cpu, int64_t mem, int64_t budget);
int64_t retired_count(void);
double scale(double x, int up);

static uint8_t mem[4096];
static struct { int64_t x[32]; int64_t pc; } cpu;
static int64_t faults;

void k_fault(int64_t pc, int64_t ins) { (void)ins; faults = pc + 1; }

/* sum 1..100 in t0, ecall with a7=1 (report), then a7=2 (exit), then garbage */
static const uint32_t guest[] = {
	0x00000293, /*  0 addi t0, zero, 0   */
	0x06400313, /*  4 addi t1, zero, 100 */
	0x006282b3, /*  8 add  t0, t0, t1    */
	0xfff30313, /* 12 addi t1, t1, -1    */
	0xfe031ce3, /* 16 bne  t1, zero, -8  */
	0x00028513, /* 20 addi a0, t0, 0     */
	0x00100893, /* 24 addi a7, zero, 1   */
	0x00000073, /* 28 ecall              */
	0x00200893, /* 32 addi a7, zero, 2   */
	0x00000073, /* 36 ecall              */
	0xffffffff, /* 40 illegal            */
};

static int64_t reported, slices;

__attribute__((export_name("boot"))) int64_t boot(void) {
	for (int i = 0; i < (int)(sizeof guest / sizeof *guest); i++) ((uint32_t *)mem)[i] = guest[i];
	for (;;) {
		/* a small budget, so the core is preempted and resumed many times */
		int64_t why = run((int64_t)(intptr_t)&cpu, (int64_t)(intptr_t)mem, 7);
		slices++;
		if (why == 0) return -1;
		if (why == 1) {
			if (cpu.x[17] == 1) reported = cpu.x[10];
			if (cpu.x[17] == 2) break;
		}
	}
	/* carry on into the illegal instruction: the core calls back into C */
	if (run((int64_t)(intptr_t)&cpu, (int64_t)(intptr_t)mem, 7) != 0) return -2;
	return reported;
}
__attribute__((export_name("stat"))) int64_t stat_(int which) {
	return which == 0 ? slices : which == 1 ? faults : retired_count();
}
__attribute__((export_name("float_check"))) int float_check(void) {
	return scale(4.0, 1) == 10.0 && scale(10.0, 0) == 4.0;
}
