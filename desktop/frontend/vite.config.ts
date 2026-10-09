import { mkdirSync, writeFileSync } from 'node:fs'
import tailwindcss from '@tailwindcss/vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vitest/config'

const preserveEmbedPlaceholder = {
  name: 'preserve-go-embed-placeholder',
  closeBundle() {
    mkdirSync(new URL('./dist', import.meta.url), { recursive: true })
    writeFileSync(new URL('./dist/.gitkeep', import.meta.url), '')
  }
}

export default defineConfig({
  plugins: [tailwindcss(), svelte(), preserveEmbedPlaceholder],
  resolve: {
    conditions: ['browser']
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts']
  }
})
