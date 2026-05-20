<template>
  <div class="rich-message" :class="[message.role, message.taskList.status]">
    <TaskListBlock
      v-if="message.taskList"
      :taskList="message.taskList"
      :expanded="taskListExpanded"
      @toggle="taskListExpanded = !taskListExpanded"
    />

    <div v-if="message.shells.length > 0" class="shells-container">
      <ShellBlock
        v-for="(shell, idx) in message.shells"
        :key="'s' + idx + '_' + shell.timestamp"
        :shell="shell"
        :auto-scroll="shell.status === 'running'"
      />
    </div>

    <div v-if="message.result && (message.result.text || message.result.codeBlocks?.length)" class="result-block">
      <div class="result-text" v-html="renderedText"></div>
      <div v-for="(cb, idx) in message.result.codeBlocks" :key="'cb'+idx" class="code-block">
        <div class="code-header">
          <span>{{ cb.lang }}</span>
          <button class="copy-btn" @click="copyCode(cb.code)">📋 复制</button>
        </div>
        <pre><code :class="'lang-'+cb.lang">{{ cb.code }}</code></pre>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import type { RichMessage } from '../../types/rich-message'
import TaskListBlock from './TaskListBlock.vue'
import ShellBlock from './ShellBlock.vue'

const props = defineProps<{ message: RichMessage }>()

const taskListExpanded = ref(true)

const renderedText = computed(() => {
  if (!props.message.result?.text) return ''
  let t = props.message.result.text
  t = t.replace(/@USR/gi, '<span class="mention">@USR</span>')
  t = t.replace(/@SE/gi, '<span class="mention">@SE</span>')
  t = t.replace(/@AP/gi, '<span class="mention">@AP</span>')
  return t.replace(/\n/g, '<br/>')
})

function copyCode(code: string) {
  navigator.clipboard.writeText(code)
}
</script>

<style scoped>
.rich-message {
  border-radius: 8px;
  padding: 8px;
}
.shells-container { margin-top: 6px; }
.result-block { margin-top: 10px; padding: 10px; border-radius: 6px; background: rgba(0,0,0,0.02); }
.result-text { line-height: 1.65; font-size: 13.5px; white-space: pre-wrap; }
.mention { color: #3b82f6; font-weight: 600; }
.code-block { margin-top: 8px; border-radius: 4px; overflow: hidden; border: 1px solid #e5e7eb; }
.code-header {
  display: flex; justify-content: space-between;
  align-items: center; padding: 4px 10px;
  background: #f3f4f6; font-size: 11px; color: #666;
}
.copy-btn { cursor: pointer; font-size: 12px; background: none; border: none; }
.code-block pre { margin: 0; }
.code-block code {
  display: block; padding: 10px; font-family: 'Cascadia Code', Consolas, monospace;
  font-size: 12px; line-height: 1.45; overflow-x: auto;
}

.rich-message.pm .result-block { border-left: 3px solid #8b5cf6; }
.rich-message.se .result-block { border-left: 3px solid #22c55e; }
.rich-message.ap .result-block { border-left: 3px solid #f59e0b; }
.rich-message.sys_c .result-block { border-left: 3px solid #666; }

.rich-message.running { animation: pulse-border 2s ease infinite; }
@keyframes pulse-border {
  0%, 100% { box-shadow: 0 0 0 0 rgba(59,130,246,0.15); }
  50% { box-shadow: 0 0 0 4px rgba(59,130,246,0.05); }
}
</style>
