package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// ========== 文档处理工具实现 ==========
// 策略：通过 Python 子进程调用专业库处理文档
// 优势：
//   - pdfplumber / pymupdf 是业界最强的 PDF 解析库（远超 Go 原生方案）
//   - python-docx 是 Word 处理的标准库，支持完整格式保留
//   - pytesseract 提供 OCR 能力，可处理扫描版/图片型 PDF
//   - AI 可以通过 ensure_tool 自动安装缺失的依赖

// DocToolResult 文档操作结果
type DocToolResult struct {
	Success bool        `json:"success"`
	Content string      `json:"content,omitempty"`
	Paths   []string    `json:"paths,omitempty"`
	Error   string      `json:"error,omitempty"`
	Meta    DocMeta     `json:"meta,omitempty"`
}

// DocMeta 文档元信息
type DocMeta struct {
	Pages       int      `json:"pages,omitempty"`
	Format      string   `json:"format,omitempty"`
	Size        int64    `json:"size,omitempty"`
	HasOCR      bool     `json:"has_ocr,omitempty"`
	WordCount   int      `json:"word_count,omitempty"`
	Tables      int      `json:"tables,omitempty"`
	Images      int      `json:"images,omitempty"`
}

// ToolRequirement 工具依赖要求
type ToolRequirement struct {
	Name        string   // 工具名称（如 "python-docx"）
	CheckCmd    string   // 检测命令（如 "python -c 'import docx'"）
	InstallCmd  string   // 安装命令（如 "pip install python-docx"）
	Description string   // 用途描述
	Optional    bool     // 是否可选（可选缺失不报错）
}

// 全局文档工具依赖表
var docToolDependencies = map[string][]ToolRequirement{
	"read_pdf": {
		{Name: "pymupdf", CheckCmd: "python -c \"import fitz\"", InstallCmd: "pip install pymupdf", Description: "PDF 文本提取引擎"},
	},
	"read_pdf_ocr": {
		{Name: "pytesseract", CheckCmd: "python -c \"import pytesseract\"", InstallCmd: "pip install pytesseract pdf2image Pillow", Description: "OCR 引擎 + PDF 转图片"},
	},
	"read_docx": {
		{Name: "python-docx", CheckCmd: "python -c \"import docx\"", InstallCmd: "pip install python-docx", Description: "Word 文档读写库"},
	},
	"write_docx": {
		{Name: "python-docx", CheckCmd: "python -c \"import docx\"", InstallCmd: "pip install python-docx", Description: "Word 文档生成库"},
	},
	"compare_docs": {
		{Name: "pymupdf", CheckCmd: "python -c \"import fitz\"", InstallCmd: "pip install pymupdf", Description: "PDF 比较引擎"},
		{Name: "python-docx", CheckCmd: "python -c \"import docx\"", InstallCmd: "pip install python-docx", Description: "Word 比较引擎"},
	},
}

// ReadPDF 读取 PDF 文件内容
// 支持普通文本型 PDF 和扫描图片型 PDF（OCR）
func ReadPDF(absPath string, useOCR bool) (*DocToolResult, error) {
	if !fileExists(absPath) {
		return nil, fmt.Errorf("文件不存在: %s", absPath)
	}

	var result *DocToolResult
	var err error

	if useOCR {
		result, err = readPDFWithOCR(absPath)
	} else {
		result, err = readPDFTextOnly(absPath)
	}

	return result, err
}

// readPDFTextOnly 使用 pymupdf 提取 PDF 纯文本
func readPDFTextOnly(absPath string) (*DocToolResult, error) {
	pythonScript := `
import sys, json, fitz  # PyMuPDF
doc = fitz.open(sys.argv[1])
pages_text = []
total_chars = 0
for i, page in enumerate(doc):
    text = page.get_text()
    pages_text.append({"page": i+1, "text": text})
    total_chars += len(text)
result = {
    "success": True,
    "content": "\\n".join([f"--- Page {p['page']} ---\\n{p['text']}" for p in pages_text]),
    "meta": {
        "pages": len(doc),
        "format": "pdf",
        "size": __import__('os').path.getsize(sys.argv[1]),
        "word_count": total_chars // 4,
    }
}
print(json.dumps(result, ensure_ascii=False))
`
	output, err := runPythonScript(pythonScript, absPath)
	if err != nil {
		return nil, fmt.Errorf("PDF解析失败: %w\n输出: %s", err, output)
	}

	var result DocToolResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		// 如果 JSON 解析失败，直接返回原始文本
		return &DocToolResult{Success: true, Content: output}, nil
	}
	return &result, nil
}

// readPDFWithOCR 使用 OCR 提取扫描版 PDF / 图片型 PDF
// 流程：PDF → 图片 → Tesseract OCR → 文本
func readPDFWithOCR(absPath string) (*DocToolResult, error) {
	pythonScript := `
import sys, json, os
try:
    import pytesseract
    from pdf2image import convert_from_path
    from PIL import Image

    images = convert_from_path(sys.argv[1], dpi=200)
    pages_text = []
    total_words = 0
    for i, img in enumerate(images):
        text = pytesseract.image_to_string(img, lang='chi_sim+eng')
        pages_text.append({"page": i+1, "text": text.strip()})
        total_words += len(text.split())

    result = {
        "success": True,
        "content": "\\n".join([f"--- Page {p['page']} (OCR) ---\\n{p['text']}" for p in pages_text]),
        "meta": {
            "pages": len(images),
            "format": "pdf",
            "size": os.path.getsize(sys.argv[1]),
            "has_ocr": True,
            "word_count": total_words,
            "images": len(images),
        }
    }
    print(json.dumps(result, ensure_ascii=False))
except ImportError as e:
    print(json.dumps({
        "success": False,
        "error": f"缺少OCR依赖: {e}\\n请运行: pip install pytesseract pdf2image Pillow\\n注意: 还需要安装系统级 Tesseract-OCR (Windows: choco install tesseract)"
    }, ensure_ascii=False))
except Exception as e:
    print(json.dumps({"success": False, "error": str(e)}, ensure_ascii=False))
`
	output, err := runPythonScript(pythonScript, absPath)
	if err != nil {
		return nil, fmt.Errorf("OCR解析失败: %w\n输出: %s", err, output)
	}

	var result DocToolResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return &DocToolResult{Success: true, Content: output}, nil
	}
	return &result, nil
}

// ReadDocx 读取 Word 文档内容
// 提取文本、表格、段落结构
func ReadDocx(absPath string) (*DocToolResult, error) {
	if !fileExists(absPath) {
		return nil, fmt.Errorf("文件不存在: %s", absPath)
	}

	pythonScript := `
import sys, json, os
from docx import Document

doc = Document(sys.argv[1])

# 提取段落文本
paragraphs = []
for p in doc.paragraphs:
    style_name = p.style.name if p.style else "Normal"
    if p.text.strip():
        paragraphs.append(f"[{style_name}] {p.text}")

# 提取表格内容
tables_data = []
for ti, table in enumerate(doc.tables):
    rows = []
    for row in table.rows:
        cells = [cell.text.strip() for cell in row.cells]
        rows.append(cells)
    tables_data.append(rows)

# 统计
word_count = sum(len(p.text.split()) for p in doc.paragraphs)

result = {
    "success": True,
    "content": "\\n".join(paragraphs),
    "meta": {
        "format": "docx",
        "size": os.path.getsize(sys.argv[1]),
        "word_count": word_count,
        "paragraphs": len(paragraphs),
        "tables": len(tables_data),
    }
}
if tables_data:
    # 将表格追加到 content 末尾
    table_texts = []
    for ti, tbl in enumerate(tables_data):
        header = " | ".join(tbl[0]) if tbl else ""
        table_texts.append(f"\\n=== Table {ti+1} ===")
        table_texts.append(header)
        for row in tbl[1:]:
            table_texts.append(" | ".join(row))
    result["content"] = result["content"] + "\\n\\n" + "\\n".join(table_texts)

print(json.dumps(result, ensure_ascii=False))
`
	output, err := runPythonScript(pythonScript, absPath)
	if err != nil {
		return nil, fmt.Errorf("DOCX解析失败: %w\n输出: %s", err, output)
	}

	var result DocToolResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return &DocToolResult{Success: true, Content: output}, nil
	}
	return &result, nil
}

// WriteDocx 写入/生成 Word 文档
// 支持：标题、正文、列表、表格、粗体/斜体等基本格式
func WriteDocx(absPath string, contentJSON string) (*DocToolResult, error) {
	// 确保 .docx 扩展名
	if !strings.HasSuffix(strings.ToLower(absPath), ".docx") {
		absPath = strings.TrimSuffix(absPath, filepath.Ext(absPath)) + ".docx"
	}

	pythonScript := `
import sys, json, os
from docx import Document
from docx.shared import Pt, Inches, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.enum.style import WD_STYLE_TYPE

doc = Document()
data = json.loads(sys.argv[1])

def add_content(doc, items):
    if isinstance(items, list):
        for item in items:
            add_content(doc, item)
    elif isinstance(items, dict):
        t = items.get("type", "paragraph")
        if t == "heading":
            level = min(max(int(items.get("level", 1)), 1), 3)
            h = doc.add_heading(items["text"], level=level)
        elif t == "paragraph":
            p = doc.add_paragraph(items["text"])
            if items.get("bold"):
                for run in p.runs: run.bold = True
            if items.get("italic"):
                for run in p.runs: run.italic = True
        elif t == "bullet_list":
            for item in items.get("items", []):
                doc.add_paragraph(item, style="List Bullet")
        elif t == "numbered_list":
            for item in items.get("items", []):
                doc.add_paragraph(item, style="List Number")
        elif t == "table":
            table = doc.add_table(rows=len(items.get("rows", [])), cols=len(items.get("rows", [[]])[0]) if items.get("rows") else 1)
            for ri, row in enumerate(items.get("rows", [])):
                for ci, cell_text in enumerate(row):
                    table.rows[ri].cells[ci].text = str(cell_text)
        elif t == "page_break":
            doc.add_page_break()

add_content(doc, data)

out_path = sys.argv[2]
os.makedirs(os.path.dirname(out_path) or ".", exist_ok=True)
doc.save(out_path)

print(json.dumps({
    "success": True,
    "paths": [out_path],
    "meta": {"format": "docx", "size": os.path.getsize(out_path)}
}, ensure_ascii=False))
`
	output, err := runPythonScript(pythonScript, contentJSON, absPath)
	if err != nil {
		return nil, fmt.Errorf("DOCX写入失败: %w\n输出: %s", err, output)
	}

	var result DocToolResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return &DocToolResult{Success: fileExists(absPath), Paths: []string{absPath}}, nil
	}
	return &result, nil
}

// CompareDocs 比较两个文档并输出差异报告
// 支持：PDF vs PDF, DOCX vs DOCX, PDF vs DOCX
func CompareDocs(pathA, pathB string) (*DocToolResult, error) {
	if !fileExists(pathA) || !fileExists(pathB) {
		return nil, fmt.Errorf("文件不存在: %s 或 %s", pathA, pathB)
	}

	extA := strings.ToLower(filepath.Ext(pathA))
	extB := strings.ToLower(filepath.Ext(pathB))

	// 根据文件类型选择比较策略
	if extA == ".pdf" && extB == ".pdf" {
		return comparePDFvsPDF(pathA, pathB)
	} else if (extA == ".docx" || extA == ".doc") && (extB == ".docx" || extB == ".doc") {
		return compareDocxvsDocx(pathA, pathB)
	} else {
		// 异构比较：先都转成文本再 diff
		return compareGeneric(pathA, pathB)
	}
}

// comparePDFvsPDF PDF 对比
func comparePDFvsPDF(pathA, pathB string) (*DocToolResult, error) {
	pythonScript := `
import sys, json, os, difflib
import fitz

def extract_pages(pdf_path):
    doc = fitz.open(pdf_path)
    return [page.get_text() for page in doc]

pages_a = extract_pages(sys.argv[1])
pages_b = extract_pages(sys.argv[2])

diff_report = []
max_pages = max(len(pages_a), len(pages_b))

for i in range(max_pages):
    text_a = pages_a[i] if i < len(pages_a) else "[FILE A 无此页]"
    text_b = pages_b[i] if i < len(pages_b) else "[FILE B 无此页]"

    if text_a.strip() == text_b.strip():
        continue

    diff = list(difflib.unified_diff(
        text_a.splitlines(keepends=True),
        text_b.splitlines(keepends=True),
        fromfile=f"A Page {i+1}",
        tofile=f"B Page {i+1}",
        lineterm=""
    ))
    if diff:
        diff_report.append(f"\\n=== Page {i+1} 差异 ===")
        diff_report.extend(diff[:100])  # 限制每页差异行数

result = {
    "success": True,
    "content": "\\n".join(diff_report) if diff_report else "两个PDF内容完全一致",
    "meta": {
        "pages_a": len(pages_a),
        "pages_b": len(pages_b),
        "differing_pages": len(diff_report),
    }
}
print(json.dumps(result, ensure_ascii=False))
`
	output, err := runPythonScript(pythonScript, pathA, pathB)
	if err != nil {
		return nil, fmt.Errorf("PDF对比失败: %w", err)
	}

	var result DocToolResult
	json.Unmarshal([]byte(output), &result)
	return &result, nil
}

// compareDocxvsDocx Word 对比
func compareDocxvsDocx(pathA, pathB string) (*DocToolResult, error) {
	pythonScript := `
import sys, json, os, difflib
from docx import Document

def extract_text(docx_path):
    doc = Document(docx_path)
    lines = []
    for p in doc.paragraphs:
        if p.text.strip():
            lines.append(p.text)
    for table in doc.tables:
        for row in table.rows:
            line = " | ".join([cell.text.strip() for cell in row.cells])
            if line.strip():
                lines.append("[TABLE] " + line)
    return lines

lines_a = extract_text(sys.argv[1])
lines_b = extract_text(sys.argv[2])

diff = list(difflib.unified_diff(
    lines_a, lines_b,
    fromfile=os.path.basename(sys.argv[1]),
    tofile=os.path.basename(sys.argv[2]),
    lineterm=""
))

result = {
    "success": True,
    "content": "\\n".join(diff) if diff else "两个Word文档内容一致",
    "meta": {
        "lines_a": len(lines_a),
        "lines_b": len(lines_b),
        "differences": len(diff),
    }
}
print(json.dumps(result, ensure_ascii=False))
`
	output, err := runPythonScript(pythonScript, pathA, pathB)
	if err != nil {
		return nil, fmt.Errorf("DOCX对比失败: %w", err)
	}

	var result DocToolResult
	json.Unmarshal([]byte(output), &result)
	return &result, nil
}

// compareGeneric 通用文档对比（先提取文本再 diff）
func compareGeneric(pathA, pathB string) (*DocToolResult, error) {
	pythonScript := `
import sys, json, os, difflib

def read_any_file(path):
    ext = os.path.splitext(path)[1].lower()
    if ext == ".pdf":
        import fitz
        doc = fitz.open(path)
        return "\\n".join([page.get_text() for page in doc])
    elif ext in (".docx", ".doc"):
        from docx import Document
        doc = Document(path)
        return "\\n".join([p.text for p in doc.paragraphs])
    else:
        with open(path, "r", encoding="utf-8", errors="ignore") as f:
            return f.read()

text_a = read_any_file(sys.argv[1])
text_b = read_any_file(sys.argv[2])

diff = list(difflib.unified_diff(
    text_a.splitlines(keepends=True),
    text_b.splitlines(keepends=True),
    fromfile=os.path.basename(sys.argv[1]),
    tofile=os.path.basename(sys.argv[2]),
    lineterm=""
))

result = {
    "success": True,
    "content": "\\n".join(diff) if diff else "两个文件内容一致",
    "meta": {"differences": len(diff)}
}
print(json.dumps(result, ensure_ascii=False))
`
	output, err := runPythonScript(pythonScript, pathA, pathB)
	if err != nil {
		return nil, fmt.Errorf("文档对比失败: %w", err)
	}

	var result DocToolResult
	json.Unmarshal([]byte(output), &result)
	return &result, nil
}

// ========== 工具自举系统 ==========

// EnsureTool 检测并确保工具可用
// 返回：(是否已就绪, 缺失的依赖列表, 安装建议)
func EnsureTool(toolName string) (ready bool, missing []string, hints []string) {
	deps, ok := docToolDependencies[toolName]
	if !ok {
		// 未注册的工具，默认就绪
		return true, nil, nil
	}

	for _, dep := range deps {
		cmd := exec.Command("cmd", "/C", dep.CheckCmd)
		cmd.SysProcAttr = getHiddenWindowAttrs()
		_, err := cmd.CombinedOutput()
		if err != nil || cmd.ProcessState.ExitCode() != 0 {
			missing = append(missing, dep.Name)
			hints = append(hints, dep.InstallCmd+"  ("+dep.Description+")")
			continue
		}
		fmt.Printf("[DocTools] ✅ %s 已就绪: %s\n", toolName, dep.Name)
	}

	return len(missing) == 0, missing, hints
}

// AutoInstallDeps 自动安装缺失的文档工具依赖
// 返回：(是否全部成功, 安装日志)
func AutoInstallDeps(toolNames ...string) (bool, string) {
	var logs []string
	allOK := true

	for _, toolName := range toolNames {
		deps, ok := docToolDependencies[toolName]
		if !ok {
			continue
		}

		for _, dep := range deps {
			// 先检查是否已经存在
			cmd := exec.Command("cmd", "/C", dep.CheckCmd)
			cmd.SysProcAttr = getHiddenWindowAttrs()
			if _, err := cmd.CombinedOutput(); err == nil {
				logs = append(logs, fmt.Sprintf("✅ %s 已存在，跳过", dep.Name))
				continue
			}

			logs = append(logs, fmt.Sprintf("📦 正在安装 %s...", dep.Name))
			installCmd := exec.Command("cmd", "/C", dep.InstallCmd)
			installCmd.SysProcAttr = getHiddenWindowAttrs()
			installCmd.Dir = "" // 使用默认工作目录
			output, err := installCmd.CombinedOutput()
			if err != nil {
				logs = append(logs, fmt.Sprintf("❌ %s 安装失败: %v\n%s", dep.Name, err, string(output)))
				allOK = false
			} else {
				logs = append(logs, fmt.Sprintf("✅ %s 安装成功", dep.Name))
			}
		}
	}

	return allOK, strings.Join(logs, "\n")
}

// CheckPythonEnv 检查 Python 环境
func CheckPythonEnv() (available bool, version string, err error) {
	cmd := exec.Command("cmd", "/C", "python --version")
	cmd.SysProcAttr = getHiddenWindowAttrs()
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 尝试 py
		cmd2 := exec.Command("cmd", "/C", "py --version")
		cmd2.SysProcAttr = getHiddenWindowAttrs()
		output2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return false, "", fmt.Errorf("未检测到Python环境。请安装Python 3.8+或确认PATH配置正确")
		}
		return true, strings.TrimSpace(string(output2)), nil
	}
	return true, strings.TrimSpace(string(output)), nil
}

// GetAvailableDocTools 返回所有可用的文档工具及其状态
func GetAvailableDocTools() map[string]interface{} {
	tools := make(map[string]interface{})
	toolNames := []string{"read_pdf", "read_pdf_ocr", "read_docx", "write_docx", "compare_docs"}

	for _, name := range toolNames {
		ready, missing, _ := EnsureTool(name)
		tools[name] = map[string]interface{}{
			"ready":   ready,
			"missing": missing,
		}
	}

	// Python 环境
	pyAvail, pyVer, _ := CheckPythonEnv()
	tools["_python"] = map[string]interface{}{
		"available": pyAvail,
		"version":   pyVer,
	}

	return tools
}

// ========== 内部辅助函数 ==========

// runPythonScript 执行 Python 脚本
// 通过 stdin 传入脚本代码，避免临时文件问题
func runPythonScript(script string, args ...string) (string, error) {
	// 确定 Python 命令
	pythonCmd := findPythonCommand()
	if pythonCmd == "" {
		return "", fmt.Errorf("未找到Python环境")
	}

	// 构建命令：echo script | python - <args>
	// Windows 下用管道方式传递脚本
	fullArgs := append([]string{"-c", script}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, pythonCmd, fullArgs...)
	cmd.SysProcAttr = getHiddenWindowAttrs()

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// findPythonCommand 查找可用的 Python 命令
func findPythonCommand() string {
	candidates := []string{"python", "python3", "py"}
	for _, c := range candidates {
		cmd := exec.Command("cmd", "/C", c+" --version")
		cmd.SysProcAttr = getHiddenWindowAttrs()
		if err := cmd.Run(); err == nil {
			return c
		}
	}
	return ""
}

// fileExists 检查文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getHiddenWindowAttrs 获取隐藏窗口的进程属性（Windows）
func getHiddenWindowAttrs() *syscall.SysProcAttr {
	if runtime.GOOS == "windows" {
		return &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
		}
	}
	return nil
}
