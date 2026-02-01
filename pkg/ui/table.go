package ui

import (
	"os"

	"github.com/olekukonko/tablewriter"
)

// RenderTable 在终端渲染一个美观的表格
func RenderTable(header []string, data [][]string) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(header)

	// 设置表格样式
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetTablePadding("\t") // 设置列间距
	table.SetNoWhiteSpace(true)

	table.AppendBulk(data)
	table.Render()
}
