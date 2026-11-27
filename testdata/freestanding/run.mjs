import { readFileSync } from "node:fs";
const mod = new WebAssembly.Module(readFileSync(process.argv[2]));
const traces = [];
const { exports: x } = new WebAssembly.Instance(mod, { env: { js_trace: (n) => traces.push(Number(n)) } });
console.log(JSON.stringify({
	imports: WebAssembly.Module.imports(mod).map((i) => `${i.module}.${i.name}`),
	boot: Number(x.boot()),
	slices: Number(x.stat(0)),
	faultAt: Number(x.stat(1)),
	retired: Number(x.stat(2)),
	retiredDirect: Number(x.retired_count()),
	floats: x.float_check(),
	traces,
}));
