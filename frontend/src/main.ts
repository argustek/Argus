import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'
import './style.css'

const app = createApp(App)
app.use(i18n)

app.config.errorHandler = (err, instance, info) => {
	console.error('[Global Error]', err)
	console.error('[Component]', instance?.$options?.name)
	console.error('[Info]', info)

	const errorInfo = {
		type: 'frontend-error',
		message: err instanceof Error ? err.message : String(err),
		stack: err instanceof Error ? err.stack : '',
		component: instance?.$options?.name || 'unknown',
		info: info,
		timestamp: new Date().toISOString(),
		url: window.location.href,
	}

	try {
		localStorage.setItem('argus-last-error', JSON.stringify(errorInfo))
	} catch (e) {
		console.warn('[Global Error] 无法保存错误信息到 localStorage')
	}
}

app.config.warningHandler = (msg, instance, trace) => {
	console.warn('[Vue Warning]', msg)
	console.warn('[Trace]', trace)
}

window.addEventListener('unhandledrejection', (event) => {
	console.error('[Unhandled Promise Rejection]', event.reason)
	event.preventDefault()
})

window.addEventListener('error', (event) => {
	if (event.error) {
		console.error('[Uncaught Error]', event.error)
	}
})

app.mount('#app')
