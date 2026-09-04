import path from 'path';

export async function buildClientBundle(): Promise<void> {
  const result = await Bun.build({
    entrypoints: [path.join(import.meta.dir, 'client', 'index.tsx')],
    outdir: path.join(process.cwd(), 'public', 'dist'),
    target: 'browser',
    minify: process.env.NODE_ENV === 'production',
    naming: 'client.js',
  });

  if (!result.success) {
    console.error('Client build failed:', result.logs);
    throw new Error('Falha ao compilar bundle do cliente.');
  }
}

if (import.meta.main) {
  console.log('Compiling client bundle...');
  await buildClientBundle();
  console.log('Client bundle ready at public/dist/client.js');
}
