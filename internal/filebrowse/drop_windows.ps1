$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
Add-Type -AssemblyName System.Windows.Forms

$form = [Windows.Forms.Form]::new()
$form.Text = '选择本机文件或目录'
$form.ClientSize = [Drawing.Size]::new(360, 110)
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.TopMost = $true
$form.KeyPreview = $true
$form.Add_KeyDown({ if ($_.KeyCode -eq 'Escape') { $form.Close() } })

$label = [Windows.Forms.Label]::new()
$label.Text = '拖入一个文件或目录'
$label.Dock = 'Fill'
$label.TextAlign = 'MiddleCenter'
$label.Font = [Drawing.Font]::new('Microsoft YaHei UI', 11)
$form.Controls.Add($label)

$target = [Windows.Forms.DataFormats]::FileDrop
foreach ($control in @($form, $label)) {
    $control.AllowDrop = $true
    $control.Add_DragEnter({
        $_.Effect = 'None'
        if ($_.Data.GetDataPresent($target, $false) -and @($_.Data.GetData($target, $false)).Count -eq 1) {
            if ($_.AllowedEffect -band [Windows.Forms.DragDropEffects]::Link) { $_.Effect = 'Link' }
            elseif ($_.AllowedEffect -band [Windows.Forms.DragDropEffects]::Copy) { $_.Effect = 'Copy' }
        }
    })
    $control.Add_DragDrop({
        if ($_.Data.GetDataPresent($target, $false)) {
            $paths = @($_.Data.GetData($target, $false))
            if ($paths.Count -eq 1 -and ([IO.File]::Exists($paths[0]) -or [IO.Directory]::Exists($paths[0]))) {
                $form.Tag = $paths[0]
                $form.Close()
            }
        }
    })
}

[void]$form.ShowDialog()
if ($form.Tag) { [Console]::WriteLine($form.Tag) }
$form.Dispose()
