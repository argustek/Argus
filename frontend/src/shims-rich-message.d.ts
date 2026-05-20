import type { RichMessage } from './types/rich-message'

declare global {
  interface Window {
    __richMessages: Record<string, RichMessage>
  }
}
