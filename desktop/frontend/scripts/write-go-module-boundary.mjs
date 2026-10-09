import { writeFile } from 'node:fs/promises'

const moduleFile = new URL('../node_modules/go.mod', import.meta.url)
const contents = `module github.com/addodelgrossi/ovm/desktop/frontend/node_modules

go 1.27.0
`

await writeFile(moduleFile, contents, { encoding: 'utf8' })
