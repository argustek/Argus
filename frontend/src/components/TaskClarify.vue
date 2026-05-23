<template>
  <div v-if="visible" class="task-clarify">
    <div class="clarify-header">
      <span class="clarify-icon">📋</span>
      <span class="clarify-title">{{ isFollowUp ? '补充确认' : '任务需求确认' }}</span>
    </div>

    <div class="clarify-body">
      <div v-for="(q, idx) in questions" :key="idx" class="question-item">
        <label class="question-text">{{ idx + 1 }}. {{ q.text }}</label>
        
        <div v-if="q.type === 'checkbox'" class="options-group">
          <label v-for="(opt, oi) in q.options" :key="oi" class="option-label">
            <input type="checkbox" :value="opt.value" v-model="answers[idx]" />
            <span>{{ opt.label }}</span>
          </label>
        </div>

        <div v-else-if="q.type === 'radio'" class="options-group">
          <label v-for="(opt, oi) in q.options" :key="oi" class="option-label">
            <input type="radio" :value="opt.value" :name="'q' + idx" v-model="answers[idx]" />
            <span>{{ opt.label }}</span>
          </label>
        </div>

        <textarea v-else-if="q.type === 'text'" 
                  class="text-input"
                  v-model="answers[idx]"
                  :placeholder="q.placeholder || '请输入...'"
                  rows="2"></textarea>
      </div>

      <div v-if="customInput" class="custom-input-item">
        <label class="question-text">其他要求（可选）:</label>
        <textarea v-model="customAnswer" 
                  class="text-input"
                  placeholder="如有其他特殊要求，请在此填写..."
                  rows="2"></textarea>
      </div>
    </div>

    <div class="clarify-footer">
      <button class="btn-submit" @click="submit" :disabled="!canSubmit">
        {{ submitLabel }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

export interface ClarifyQuestion {
  text: string
  type: 'checkbox' | 'radio' | 'text'
  options?: Array<{ label: string; value: string }>
  placeholder?: string
}

const props = defineProps<{
  visible: boolean
  questions: ClarifyQuestion[]
  isFollowUp?: boolean
}>()

const emit = defineEmits<{
  (e: 'submit', answers: Record<number, string | string[]>, custom?: string): void
}>()

const answers = ref<Record<number, string | string[]>>({})
const customAnswer = ref('')
const customInput = ref(true)

const submitLabel = computed(() => props.isFollowUp ? '确认' : '下一步')

const canSubmit = computed(() => {
  if (!props.questions.length) return false
  return true
})

function submit() {
  const result: Record<number, string | string[]> = {}
  for (const [k, v] of Object.entries(answers.value)) {
    result[Number(k)] = v
  }
  emit('submit', result, customAnswer.value || undefined)
}

function reset() {
  answers.value = {}
  customAnswer.value = ''
}

defineExpose({ reset })
</script>

<style scoped>
.task-clarify {
  margin: 8px 16px;
  padding: 14px;
  background: rgba(59, 130, 246, 0.06);
  border: 1px solid rgba(59, 130, 246, 0.18);
  border-radius: 10px;
  font-size: 13px;
}
.clarify-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid rgba(128,128,128,0.12);
}
.clarify-icon { font-size: 15px; flex-shrink: 0; }
.clarify-title { font-weight: 600; color: #374151; }
.question-item { margin-bottom: 12px; }
.question-text { display: block; margin-bottom: 6px; color: #4b5563; font-weight: 500; font-size: 12px; }
.options-group { display: flex; flex-wrap: wrap; gap: 12px; }
.option-label {
  display: flex; align-items: center; gap: 5px; cursor: pointer; font-size: 12px; color: #555;
}
.option-label input { accent-color: #3b82f6; }
.text-input {
  width: 100%; padding: 7px 10px;
  border: 1px solid rgba(128,128,128,0.25); border-radius: 6px;
  font-size: 12px; resize: vertical; outline: none;
  background: rgba(255,255,255,0.7);
}
.text-input:focus { border-color: #3b82f6; box-shadow: 0 0 0 2px rgba(59,130,246,0.12); }
.custom-input-item { margin-top: 10px; padding-top: 10px; border-top: 1px dashed rgba(128,128,128,0.15); }
.clarify-footer { margin-top: 12px; text-align: right; }
.btn-submit {
  padding: 7px 22px;
  background: #3b82f6; color: white; border: none; border-radius: 6px;
  font-size: 12px; font-weight: 600; cursor: pointer;
}
.btn-submit:hover { background: #2563eb; }
.btn-submit:disabled { opacity: 0.5; cursor: not-allowed; }
</style>