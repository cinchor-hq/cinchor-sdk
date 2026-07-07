import { defineConfig } from 'tsup';

export default defineConfig({
  entry: ['src/index.ts'],
  format: ['esm', 'cjs'],
  dts: true,
  sourcemap: true,
  clean: true,
  target: 'es2022',
  // @omne/sdk is a real runtime dependency, not bundled into our output.
  external: ['@omne/sdk', '@scure/base'],
});
