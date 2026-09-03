export function handler(env: WorkerEnv): string {
	return WORKER_BUILD_ID + ":" + env.bucket;
}
