import { reactive, onMounted, onUnmounted } from 'vue'

export function useDraggable(initialX: number, initialY: number, initialW: number = 600, initialH: number = 350) {
  const windowPos = reactive({ x: initialX, y: initialY })
  const windowSize = reactive({ w: initialW, h: initialH })
  
  let isDragging = false
  let isResizing = false
  let resizeDir = ''
  let dragStart = { x: 0, y: 0 }
  let windowStart = { x: 0, y: 0 }
  let sizeStart = { w: 0, h: 0 }

  function startDrag(e: MouseEvent) {
    isDragging = true
    dragStart.x = e.clientX
    dragStart.y = e.clientY
    windowStart.x = windowPos.x
    windowStart.y = windowPos.y
    e.preventDefault()
    e.stopPropagation()
  }

  function startResize(e: MouseEvent, direction: string) {
    isResizing = true
    resizeDir = direction
    dragStart.x = e.clientX
    dragStart.y = e.clientY
    windowStart.x = windowPos.x
    windowStart.y = windowPos.y
    sizeStart.w = windowSize.w
    sizeStart.h = windowSize.h
    e.preventDefault()
    e.stopPropagation()
  }

  function onMove(e: MouseEvent) {
    if (isDragging) {
      const deltaX = e.clientX - dragStart.x
      const deltaY = e.clientY - dragStart.y
      windowPos.x = windowStart.x + deltaX
      windowPos.y = windowStart.y + deltaY
    }
    
    if (isResizing) {
      const deltaX = e.clientX - dragStart.x
      const deltaY = e.clientY - dragStart.y
      const minW = 300
      const minH = 200
      
      if (resizeDir.includes('e')) {
        windowSize.w = Math.max(minW, sizeStart.w + deltaX)
      }
      if (resizeDir.includes('s')) {
        windowSize.h = Math.max(minH, sizeStart.h + deltaY)
      }
      if (resizeDir.includes('w')) {
        const newW = Math.max(minW, sizeStart.w - deltaX)
        if (newW > minW) {
          windowSize.w = newW
          windowPos.x = windowStart.x + deltaX
        }
      }
      if (resizeDir.includes('n')) {
        const newH = Math.max(minH, sizeStart.h - deltaY)
        if (newH > minH) {
          windowSize.h = newH
          windowPos.y = windowStart.y + deltaY
        }
      }
    }
  }

  function stopInteraction() {
    isDragging = false
    isResizing = false
  }

  onMounted(() => {
    document.addEventListener('mousemove', onMove, { passive: true })
    document.addEventListener('mouseup', stopInteraction)
  })

  onUnmounted(() => {
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', stopInteraction)
  })

  return { 
    windowPos, 
    windowSize, 
    startDrag, 
    startResize 
  }
}
