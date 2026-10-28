param(
    [Parameter(Mandatory=$true)][int]$AppProcessId,
    [Parameter(Mandatory=$true)][string]$IconGuid,
    [ValidateSet('snapshot', 'open', 'select', 'close', 'minimise', 'exit')][string]$Action = 'snapshot'
)
$ErrorActionPreference = 'Stop'
Add-Type @'
using System;
using System.Text;
using System.Runtime.InteropServices;
public static class TraySmokeWindow {
    private delegate bool EnumCallback(IntPtr window, IntPtr data);
    [DllImport("user32.dll")] private static extern bool EnumWindows(EnumCallback callback, IntPtr data);
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr window, out uint process);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] private static extern int GetClassName(IntPtr window, StringBuilder text, int length);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr window);
    [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr window, uint message, IntPtr wParam, IntPtr lParam);
    [DllImport("user32.dll", SetLastError=true)] public static extern IntPtr SendMessageTimeout(IntPtr window, uint message, IntPtr wParam, IntPtr lParam, uint flags, uint timeout, out IntPtr result);
    [StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)] private struct IconData {
        public uint Size; public IntPtr Window; public uint ID, Flags, Callback;
        public IntPtr Icon;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst=128)] public string Tip;
        public uint State, StateMask;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst=256)] public string Info;
        public uint Version;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst=64)] public string InfoTitle;
        public uint InfoFlags; public Guid GUID; public IntPtr BalloonIcon;
    }
    [DllImport("shell32.dll", CharSet=CharSet.Unicode)] private static extern bool Shell_NotifyIcon(uint operation, ref IconData data);
    public static bool Registered(IntPtr window, string guid) {
        if (window == IntPtr.Zero) return false;
        var data = new IconData { Window=window, ID=1, Flags=0x20, GUID=new Guid(guid) };
        data.Size=(uint)Marshal.SizeOf(data);
        // NIM_MODIFY with only NIF_GUID checks registration without changing the icon.
        return Shell_NotifyIcon(1, ref data);
    }
    public static IntPtr Find(uint process, bool tray) {
        IntPtr found=IntPtr.Zero;
        EnumWindows((window,data) => {
            uint owner; GetWindowThreadProcessId(window, out owner);
            if (owner != process) return true;
            var text=new StringBuilder(256); GetClassName(window,text,256);
            if (tray ? text.ToString().StartsWith("ScanSSH-ray.Tray.") : text.ToString()=="wailsWindow") {
                found=window; return false;
            }
            return true;
        },IntPtr.Zero);
        return found;
    }
}
'@
$MainWindow = [TraySmokeWindow]::Find($AppProcessId, $false)
$TrayWindow = [TraySmokeWindow]::Find($AppProcessId, $true)
if ($MainWindow -eq [IntPtr]::Zero) { throw 'The tested process has no Wails window.' }
$TargetWindow = $TrayWindow
$Message = 0x111
$WParam = 0
$LParam = 0
switch ($Action) {
    'open' { $WParam = 1 }
    'select' { $Message = 0x8001; $LParam = 0x10400 }
    'close' { $TargetWindow = $MainWindow; $Message = 0x112; $WParam = 0xF060 }
    'minimise' { $TargetWindow = $MainWindow; $Message = 0x112; $WParam = 0xF020 }
    'exit' {
        if (-not [TraySmokeWindow]::PostMessage($TrayWindow, 0x111, [IntPtr]2, [IntPtr]::Zero)) { throw 'Cannot request tray Exit.' }
        @{ action='exit' } | ConvertTo-Json -Compress
        return
    }
}
if ($Action -ne 'snapshot') {
    $MessageResult = [IntPtr]::Zero
    if ([TraySmokeWindow]::SendMessageTimeout($TargetWindow, $Message, [IntPtr]$WParam, [IntPtr]$LParam, 2, 2000, [ref]$MessageResult) -eq [IntPtr]::Zero) {
        throw "Native action timed out or failed: $Action"
    }
}
@{
    visible=[TraySmokeWindow]::IsWindowVisible($MainWindow)
    tray=$TrayWindow.ToInt64()
    registered=[TraySmokeWindow]::Registered($TrayWindow, $IconGuid)
} | ConvertTo-Json -Compress
