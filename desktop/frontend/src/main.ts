import './style.css'
import { mount } from 'svelte'
import App from './App.svelte'
import { applyStoredTheme } from './lib/theme'

applyStoredTheme()

const target = document.getElementById('app')
if (!target) {
  throw new Error('application mount point not found')
}

const app = mount(App, { target })

export default app
