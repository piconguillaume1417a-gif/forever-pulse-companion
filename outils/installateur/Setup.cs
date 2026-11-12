using System;
using System.Diagnostics;
using System.Drawing;
using System.Globalization;
using System.IO;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Forms;

// Version attributes come from the generated PackageInfo.cs (Build.ps1).
[assembly: AssemblyTitle("Forever Pulse Companion Setup")]
[assembly: AssemblyProduct("Forever Pulse Companion")]
[assembly: AssemblyCompany("Forever Pulse")]

internal static class Program
{
    // French or Korean when Windows is in that language, English otherwise (0.10.0 ; rc.17 : Korean).
    internal static readonly string Language = CultureInfo.CurrentUICulture.TwoLetterISOLanguageName;
    internal static readonly bool French = Language == "fr";
    internal static readonly bool Korean = Language == "ko";
    internal static string T(string fr, string en, string ko) { return French ? fr : Korean ? ko : en; }

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    internal static extern IntPtr FindWindow(string className, string title);
    [DllImport("msi.dll", CharSet = CharSet.Unicode)]
    internal static extern uint MsiVerifyPackage(string package);
    [DllImport("msi.dll", CharSet = CharSet.Unicode)]
    internal static extern int MsiQueryProductState(string product);
    [DllImport("ntdll.dll", CharSet = CharSet.Unicode)]
    private static extern int RtlGetVersion(ref OsVersion version);
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct OsVersion
    {
        internal uint Size, Major, Minor, Build, Platform;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 128)] internal string ServicePack;
    }

    internal static string InstallDirectory
    {
        get { return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Programs", "ForeverPulseCompanion"); }
    }
    internal static bool Windows11()
    {
        OsVersion os = new OsVersion();
        os.Size = (uint)Marshal.SizeOf(typeof(OsVersion));
        return Environment.Is64BitOperatingSystem && RtlGetVersion(ref os) == 0 && os.Major >= 10 && os.Build >= 22000;
    }
    internal static bool CompanionRunning()
    {
        return FindWindow("ForeverPulseCompanionTray", null) != IntPtr.Zero;
    }
    // Asks the running companion to quit cleanly (its own --quit: icon removed,
    // database closed), then waits up to 30 s. Never kills a process.
    internal static bool QuitCompanion()
    {
        string exe = Path.Combine(InstallDirectory, "ForeverPulseCompanion.exe");
        try
        {
            if (File.Exists(exe))
                using (Process p = Process.Start(new ProcessStartInfo(exe, "--quit") { UseShellExecute = false, CreateNoWindow = true }))
                    p.WaitForExit(10000);
        }
        catch (Exception) { return false; }
        for (int i = 0; i < 60 && CompanionRunning(); i++) Thread.Sleep(500);
        if (CompanionRunning()) return false;
        Thread.Sleep(1500); // let the process finish closing its database
        return true;
    }
    internal static string Hash(string file)
    {
        using (SHA256 sha = SHA256.Create())
        using (Stream input = File.OpenRead(file))
            return BitConverter.ToString(sha.ComputeHash(input)).Replace("-", "");
    }
    internal static void ExtractPackage(string file)
    {
        using (Stream input = Assembly.GetExecutingAssembly().GetManifestResourceStream("companion.msi"))
        using (Stream output = new FileStream(file, FileMode.CreateNew, FileAccess.Write, FileShare.None))
        {
            if (input == null) throw new InvalidDataException(T("Le paquet d'installation est absent.", "The installation package is missing.", "설치 패키지가 없습니다."));
            input.CopyTo(output);
        }
        if (Hash(file) != PackageInfo.MsiHash || MsiVerifyPackage(file) != 0)
            throw new InvalidDataException(T("Le paquet d'installation est endommagé. Téléchargez de nouveau l'installateur.", "The installation package is damaged. Download the installer again.", "설치 패키지가 손상되었습니다. 설치 프로그램을 다시 다운로드하세요."));
    }

    [STAThread]
    private static int Main(string[] args)
    {
        // Verification uses read/search/costing APIs only, never an install transaction.
        if (args.Length == 2 && args[0] == "--verify")
        {
            try
            {
                string folder = Path.GetFullPath(args[1]);
                Directory.CreateDirectory(folder);
                ExtractPackage(Path.Combine(folder, "companion.msi"));
                InspectMsi.Extract(Path.Combine(folder, "companion.msi"), Path.Combine(folder, "companion.cab"));
                InspectMsi.CheckPlanning(Path.Combine(folder, "companion.msi"), Path.Combine(folder, "planning.txt"));
                File.WriteAllText(Path.Combine(folder, "verification.txt"), "Version: " + PackageInfo.Version + "\r\nWindows11 x64: " + Windows11() + "\r\nMSI SHA256: " + PackageInfo.MsiHash + "\r\nCompanion SHA256: " + PackageInfo.CompanionHash + "\r\nMsiVerifyPackage: OK\r\nInstallation executed: false\r\n");
                return 0;
            }
            catch (Exception e) { File.WriteAllText(Path.Combine(args[1], "error.txt"), e.ToString()); return 1; }
        }
        if (args.Length == 3 && args[0] == "--check-msi")
        {
            try { InspectMsi.CheckPlanning(Path.GetFullPath(args[1]), Path.GetFullPath(args[2])); return 0; }
            catch (Exception) { return 1; }
        }
        Application.EnableVisualStyles();
        Application.SetCompatibleTextRenderingDefault(false);
        using (SetupWindow window = new SetupWindow())
        {
            if (args.Length == 2 && args[0] == "--preview")
            {
                // Show invisibly so WinForms creates/layouts every child control.
                window.ShowInTaskbar = false;
                window.Opacity = 0;
                window.Show();
                window.Update();
                using (Bitmap bitmap = new Bitmap(window.Width, window.Height))
                {
                    window.DrawToBitmap(bitmap, new Rectangle(0, 0, bitmap.Width, bitmap.Height));
                    bitmap.Save(Path.GetFullPath(args[1]), System.Drawing.Imaging.ImageFormat.Png);
                }
                return 0;
            }
            if (args.Length != 0) return 2;
            Application.Run(window);
        }
        return 0;
    }
}

// A selectable, read-only path that grows to its native wrapped line count.
// Measuring the Edit control itself also handles long words and DPI rounding.
internal sealed class InstallPathBox : TextBox
{
    internal InstallPathBox()
    {
        Multiline = true;
        WordWrap = true;
        AutoSize = false;
    }

    private void FitText()
    {
        if (!IsHandleCreated || ClientSize.Width <= 0) return;
        int lines = GetLineFromCharIndex(TextLength) + 1;
        int wanted = Math.Max(2, lines) * Font.Height + (Height - ClientSize.Height) + 8;
        if (Height != wanted) Height = wanted;
    }

    protected override void OnHandleCreated(EventArgs e) { base.OnHandleCreated(e); FitText(); }
    protected override void OnTextChanged(EventArgs e) { base.OnTextChanged(e); FitText(); }
    protected override void OnFontChanged(EventArgs e) { base.OnFontChanged(e); FitText(); }
    protected override void OnResize(EventArgs e) { base.OnResize(e); FitText(); }
}

internal sealed class SetupWindow : Form
{
    private readonly Button install = new Button();
    private readonly Button close = new Button();
    private readonly CheckBox desktop = new CheckBox();
    private readonly Label status = new Label();
    private readonly ProgressBar progress = new ProgressBar();
    private bool installing, complete;

    internal SetupWindow()
    {
        SuspendLayout();
        Text = Program.T("Installation · Forever Pulse Companion ", "Setup · Forever Pulse Companion ", "설치 · Forever Pulse Companion ") + PackageInfo.Version;
        AutoScaleDimensions = new SizeF(96F, 96F);
        AutoScaleMode = AutoScaleMode.Dpi;
        ClientSize = new Size(650, 540);
        // Korean: Malgun Gothic, shipped with Windows (Segoe UI has no hangul).
        Font = new Font(Program.Korean ? "Malgun Gothic" : "Segoe UI", 10);
        MinimumSize = new Size(550, 480);
        FormBorderStyle = FormBorderStyle.Sizable;
        StartPosition = FormStartPosition.CenterScreen;
        MaximizeBox = false;
        BackColor = Color.FromArgb(247, 248, 251);

        TableLayoutPanel layout = new TableLayoutPanel();
        layout.Dock = DockStyle.Fill;
        layout.ColumnCount = 1;
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        layout.RowCount = 3;
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        layout.RowStyles.Add(new RowStyle(SizeType.Percent, 100));
        layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
        Controls.Add(layout);

        TableLayoutPanel header = new TableLayoutPanel();
        header.AutoSize = true;
        header.Dock = DockStyle.Fill;
        header.Margin = Padding.Empty;
        header.Padding = new Padding(28, 22, 28, 20);
        header.ColumnCount = 2;
        header.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 76));
        header.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        header.BackColor = Color.FromArgb(24, 34, 52);
        layout.Controls.Add(header, 0, 0);
        using (Stream source = Assembly.GetExecutingAssembly().GetManifestResourceStream("logo.png"))
        {
            PictureBox logo = new PictureBox();
            logo.Size = new Size(60, 60);
            logo.Margin = Padding.Empty;
            logo.SizeMode = PictureBoxSizeMode.Zoom;
            logo.Image = new Bitmap(source);
            header.Controls.Add(logo, 0, 0);
            header.SetRowSpan(logo, 2);
        }
        Label title = new Label();
        title.AutoSize = true;
        title.Dock = DockStyle.Fill;
        title.Margin = new Padding(0, 0, 0, 8);
        title.Font = new Font(Font.FontFamily, 18, FontStyle.Bold);
        title.ForeColor = Color.White;
        title.Text = "Forever Pulse Companion";
        header.Controls.Add(title, 1, 0);
        Label subtitle = new Label();
        subtitle.AutoSize = true;
        subtitle.Dock = DockStyle.Fill;
        subtitle.Margin = Padding.Empty;
        subtitle.ForeColor = Color.FromArgb(193, 206, 228);
        subtitle.Text = "Version " + PackageInfo.Version + " · Windows 11 · 64" + Program.T(" bits", "-bit", "비트");
        header.Controls.Add(subtitle, 1, 1);

        // Rows follow their contents, including wrapped text at larger Windows scales.
        // Only the body scrolls on a short screen; the action buttons remain reachable.
        Panel viewport = new Panel();
        viewport.Dock = DockStyle.Fill;
        viewport.AutoScroll = true;
        viewport.Margin = Padding.Empty;
        layout.Controls.Add(viewport, 0, 1);
        TableLayoutPanel body = new TableLayoutPanel();
        body.Dock = DockStyle.Top;
        body.AutoSize = true;
        body.AutoSizeMode = AutoSizeMode.GrowAndShrink;
        body.ColumnCount = 1;
        body.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        body.Padding = new Padding(30, 22, 30, 8);
        viewport.Controls.Add(body);

        AddLabel(body, Program.T("Installer pour votre compte Windows", "Install for your Windows account", "Windows 계정에 설치"), true);
        AddLabel(body, Program.T("Le Compagnon et ses guides seront ajoutés ici :", "The Companion and its guides will be added here:", "Companion과 안내서가 여기에 추가됩니다:"), false);
        InstallPathBox path = new InstallPathBox();
        path.Name = "InstallPath";
        path.Dock = DockStyle.Top;
        path.Margin = new Padding(0, 0, 0, 18);
        path.Text = Program.InstallDirectory;
        path.ReadOnly = true;
        path.AccessibleName = Program.T("Dossier d'installation", "Installation folder", "설치 폴더");
        path.BackColor = Color.White;
        body.Controls.Add(path);
        AddLabel(body, Program.T("Votre configuration, votre connexion et vos relevés sont conservés.\nUn raccourci sera ajouté au menu Démarrer. Aucun droit administrateur.\nLe Compagnon se met ensuite à jour tout seul.",
            "Your settings, connection and readings are kept.\nA Start menu shortcut will be added. No administrator rights.\nThe Companion then keeps itself up to date.", "설정, 연결, 기록은 유지됩니다.\n시작 메뉴에 바로 가기가 추가됩니다. 관리자 권한은 필요하지 않습니다.\n이후 Companion은 자동으로 업데이트됩니다."), false);
        desktop.AutoSize = true;
        desktop.Dock = DockStyle.Top;
        desktop.Margin = new Padding(0, 0, 0, 18);
        desktop.Text = Program.T("Ajouter aussi un raccourci sur le Bureau", "Also add a desktop shortcut", "바탕 화면에도 바로 가기 추가");
        desktop.Checked = true;
        body.Controls.Add(desktop);
        status.AutoSize = true;
        status.Dock = DockStyle.Top;
        status.Margin = new Padding(0, 0, 0, 18);
        status.Text = Program.T("Si le Compagnon est ouvert, l'installation le fermera puis vous proposera de le rouvrir.", "If the Companion is open, setup will close it and then offer to open it again.", "Companion이 열려 있으면 설치 프로그램이 종료한 뒤 다시 열 수 있도록 안내합니다.");
        body.Controls.Add(status);
        progress.Dock = DockStyle.Top;
        progress.Height = 12;
        progress.Margin = Padding.Empty;
        progress.Visible = false;
        body.Controls.Add(progress);
        FlowLayoutPanel actions = new FlowLayoutPanel();
        actions.AutoSize = true;
        actions.Dock = DockStyle.Fill;
        actions.FlowDirection = FlowDirection.RightToLeft;
        actions.Margin = new Padding(30, 0, 30, 0);
        actions.Padding = new Padding(0, 16, 0, 24);
        layout.Controls.Add(actions, 0, 2);
        close.AutoSize = true;
        close.MinimumSize = new Size(130, 40);
        close.Margin = new Padding(0, 0, 15, 0);
        close.Text = Program.T("Fermer", "Close", "닫기");
        close.Click += delegate { Close(); };
        install.AutoSize = true;
        install.MinimumSize = new Size(133, 40);
        install.Margin = Padding.Empty;
        install.Text = Program.T("Installer", "Install", "설치");
        install.BackColor = Color.FromArgb(36, 88, 162);
        install.ForeColor = Color.White;
        install.FlatStyle = FlatStyle.Flat;
        install.Click += InstallClick;
        actions.Controls.Add(install);
        actions.Controls.Add(close);
        AcceptButton = install;
        CancelButton = close;
        ActiveControl = install;
        FormClosing += delegate(object sender, FormClosingEventArgs e) { if (installing) e.Cancel = true; };
        AutoScaleDimensions = new SizeF(96F, 96F);
        ResumeLayout(true);
    }

    private void AddLabel(TableLayoutPanel parent, string text, bool bold)
    {
        Label label = new Label();
        label.AutoSize = true;
        label.Dock = DockStyle.Top;
        label.Margin = new Padding(0, 0, 0, bold ? 18 : 12);
        label.Text = text;
        if (bold) label.Font = new Font(Font.FontFamily, 12, FontStyle.Bold);
        parent.Controls.Add(label);
    }

    private async void InstallClick(object sender, EventArgs e)
    {
        if (!complete && Program.CompanionRunning())
        {
            if (MessageBox.Show(this, Program.T("Le Compagnon est ouvert. Le fermer maintenant pour l'installer ? Aucun relevé n'est perdu.", "The Companion is running. Close it now to install? No reading is lost.", "Companion이 실행 중입니다. 지금 종료하고 설치하시겠습니까? 기록은 사라지지 않습니다."),
                    "Forever Pulse Companion", MessageBoxButtons.OKCancel, MessageBoxIcon.Question) != DialogResult.OK)
                return;
            status.Text = Program.T("Fermeture du Compagnon…", "Closing the Companion…", "Companion 종료 중…");
            install.Enabled = false;
            bool closed = await Task.Run(new Func<bool>(Program.QuitCompanion));
            install.Enabled = true;
            if (!closed)
            {
                MessageBox.Show(this, Program.T("Le Compagnon ne s'est pas fermé. Choisissez Quitter dans le menu de son icône près de l'horloge, puis réessayez.", "The Companion did not close. Choose Quit in its icon menu next to the clock, then try again.", "Companion이 종료되지 않았습니다. 시계 옆 아이콘 메뉴에서 “종료”를 선택한 다음 다시 시도하세요."),
                    "Forever Pulse Companion", MessageBoxButtons.OK, MessageBoxIcon.Information);
                return;
            }
        }
        if (complete)
        {
            try
            {
                Process.Start(new ProcessStartInfo(Path.Combine(Program.InstallDirectory, "ForeverPulseCompanion.exe")) { UseShellExecute = true });
                Close();
            }
            catch (Exception error) { MessageBox.Show(this, error.Message, Program.T("Ouverture impossible", "Cannot open", "열 수 없음"), MessageBoxButtons.OK, MessageBoxIcon.Error); }
            return;
        }
        if (!Program.Windows11())
        {
            MessageBox.Show(this, Program.T("Cet installateur nécessite Windows 11 en 64 bits.", "This installer requires 64-bit Windows 11.", "이 설치 프로그램에는 64비트 Windows 11이 필요합니다."), Program.T("Windows non compatible", "Unsupported Windows", "지원되지 않는 Windows"), MessageBoxButtons.OK, MessageBoxIcon.Error);
            return;
        }
        installing = true;
        install.Enabled = close.Enabled = desktop.Enabled = false;
        progress.Style = ProgressBarStyle.Marquee;
        progress.Visible = true;
        status.Text = Program.T("Installation en cours…", "Installing…", "설치 중…");
        string folder = Path.Combine(Path.GetTempPath(), "ForeverPulse-Setup-" + Guid.NewGuid().ToString("N"));
        string package = Path.Combine(folder, "companion.msi");
        string log = Path.Combine(folder, "installation.log");
        bool succeeded = false;
        bool wantsDesktop = desktop.Checked;
        try
        {
            Directory.CreateDirectory(folder);
            int result = await Task.Run(delegate
            {
                Program.ExtractPackage(package);
                // Per-user MSI; no elevated privileges, restart or automatic app launch.
                string repair = Program.MsiQueryProductState(PackageInfo.ProductCode) == 5 ? " REINSTALL=ALL REINSTALLMODE=vomus" : "";
                string arguments = "/i \"" + package + "\" /qn /norestart ALLUSERS=\"\" DESKTOPSHORTCUT=" + (wantsDesktop ? "1" : "0") + repair + " /L*v \"" + log + "\"";
                using (Process process = Process.Start(new ProcessStartInfo(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.System), "msiexec.exe"), arguments) { UseShellExecute = false, CreateNoWindow = true }))
                {
                    process.WaitForExit();
                    return process.ExitCode;
                }
            });
            if (result != 0 && result != 3010)
                throw new InvalidOperationException(Program.T("Windows Installer a renvoyé le code ", "Windows Installer returned code ", "Windows Installer 반환 코드: ") + result + ".\n" + Program.T("Journal : ", "Log: ", "로그: ") + log);
            string installed = Path.Combine(Program.InstallDirectory, "ForeverPulseCompanion.exe");
            if (Program.Hash(installed) != PackageInfo.CompanionHash)
                throw new InvalidDataException(Program.T("Le fichier installé ne correspond pas à la version attendue.", "The installed file does not match the expected version.", "설치된 파일이 예상한 버전과 일치하지 않습니다.") + "\n" + Program.T("Journal : ", "Log: ", "로그: ") + log);
            succeeded = complete = true;
            install.Text = Program.T("Ouvrir", "Open", "열기");
            status.Text = Program.T("Installation terminée. Cliquez sur Ouvrir pour lancer la version ", "Setup complete. Click Open to start version ", "설치가 완료되었습니다(버전 ") + PackageInfo.Version + Program.T(", ou sur Fermer pour la lancer plus tard.", ", or Close to start it later.", "). 지금 실행하려면 “열기”, 나중에 실행하려면 “닫기”를 클릭하세요.");
        }
        catch (Exception error)
        {
            status.Text = Program.T("L'installation n'a pas abouti. Vous pouvez réessayer.", "Setup did not complete. You can try again.", "설치가 완료되지 않았습니다. 다시 시도할 수 있습니다.");
            MessageBox.Show(this, error.Message, Program.T("Installation interrompue", "Setup interrupted", "설치 중단됨"), MessageBoxButtons.OK, MessageBoxIcon.Error);
        }
        finally
        {
            installing = false;
            install.Enabled = close.Enabled = true;
            desktop.Enabled = !complete;
            progress.Visible = false;
            // Only this invocation's fresh temporary directory is removed.
            if (succeeded && Directory.Exists(folder))
            {
                try { Directory.Delete(folder, true); }
                catch (IOException) { /* Windows Installer may still hold its log briefly. */ }
                catch (UnauthorizedAccessException) { /* Do not turn a successful install into an error. */ }
            }
        }
    }
}
