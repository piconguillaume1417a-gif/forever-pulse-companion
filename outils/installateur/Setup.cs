using System;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Threading.Tasks;
using System.Windows.Forms;

[assembly: AssemblyTitle("Installation de Forever Pulse Companion")]
[assembly: AssemblyProduct("Forever Pulse Companion")]
[assembly: AssemblyCompany("Forever Pulse")]
[assembly: AssemblyVersion("0.8.0.0")]
[assembly: AssemblyFileVersion("0.8.0.1")]

internal static class Program
{
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
            if (input == null) throw new InvalidDataException("Le paquet d'installation est absent.");
            input.CopyTo(output);
        }
        if (Hash(file) != PackageInfo.MsiHash || MsiVerifyPackage(file) != 0)
            throw new InvalidDataException("Le paquet d'installation est endommagé. Téléchargez de nouveau l'installateur.");
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
        Text = "Installation · Forever Pulse Companion 0.8.0";
        AutoScaleDimensions = new SizeF(96F, 96F);
        AutoScaleMode = AutoScaleMode.Dpi;
        ClientSize = new Size(650, 540);
        Font = new Font("Segoe UI", 10);
        FormBorderStyle = FormBorderStyle.FixedDialog;
        StartPosition = FormStartPosition.CenterScreen;
        MaximizeBox = false;
        BackColor = Color.FromArgb(247, 248, 251);

        Panel header = new Panel();
        header.SetBounds(0, 0, 650, 106);
        header.BackColor = Color.FromArgb(24, 34, 52);
        Controls.Add(header);
        using (Stream source = Assembly.GetExecutingAssembly().GetManifestResourceStream("logo.png"))
        {
            PictureBox logo = new PictureBox();
            logo.SetBounds(28, 23, 60, 60);
            logo.SizeMode = PictureBoxSizeMode.Zoom;
            logo.Image = new Bitmap(source);
            header.Controls.Add(logo);
        }
        Label title = new Label();
        title.SetBounds(104, 22, 500, 34);
        title.Font = new Font(Font.FontFamily, 18, FontStyle.Bold);
        title.ForeColor = Color.White;
        title.Text = "Forever Pulse Companion";
        header.Controls.Add(title);
        Label subtitle = new Label();
        subtitle.SetBounds(106, 62, 480, 24);
        subtitle.ForeColor = Color.FromArgb(193, 206, 228);
        subtitle.Text = "Version 0.8.0 · Windows 11 · 64 bits";
        header.Controls.Add(subtitle);

        AddLabel("Installer pour votre compte Windows", 30, 128, 590, 28, true);
        AddLabel("Le compagnon et ses guides seront ajoutés ici :", 30, 164, 590, 25, false);
        TextBox path = new TextBox();
        path.SetBounds(30, 193, 590, 32);
        path.Text = Program.InstallDirectory;
        path.ReadOnly = true;
        path.TabStop = false;
        path.BackColor = Color.White;
        Controls.Add(path);
        AddLabel("Votre configuration, votre jeton et vos relevés sont conservés.\nUn raccourci sera ajouté au menu Démarrer.\nLe démarrage avec Windows se règle dans le compagnon.", 30, 240, 590, 78, false);
        desktop.SetBounds(30, 324, 590, 28);
        desktop.Text = "Ajouter aussi un raccourci sur le Bureau";
        desktop.Checked = true;
        Controls.Add(desktop);
        status.SetBounds(30, 363, 590, 54);
        status.Text = "Fermez le compagnon depuis son icône près de l'horloge avant l'installation.";
        Controls.Add(status);
        progress.SetBounds(30, 427, 590, 12);
        progress.Visible = false;
        Controls.Add(progress);
        close.SetBounds(342, 467, 130, 40);
        close.Text = "Fermer";
        close.Click += delegate { Close(); };
        Controls.Add(close);
        install.SetBounds(487, 467, 133, 40);
        install.Text = "Installer";
        install.BackColor = Color.FromArgb(36, 88, 162);
        install.ForeColor = Color.White;
        install.FlatStyle = FlatStyle.Flat;
        install.Click += InstallClick;
        Controls.Add(install);
        AcceptButton = install;
        CancelButton = close;
        FormClosing += delegate(object sender, FormClosingEventArgs e) { if (installing) e.Cancel = true; };
        AutoScaleDimensions = new SizeF(96F, 96F);
        ResumeLayout(false);
    }

    private void AddLabel(string text, int x, int y, int width, int height, bool bold)
    {
        Label label = new Label();
        label.SetBounds(x, y, width, height);
        label.Text = text;
        if (bold) label.Font = new Font(Font.FontFamily, 12, FontStyle.Bold);
        Controls.Add(label);
    }

    private async void InstallClick(object sender, EventArgs e)
    {
        if (Program.CompanionRunning())
        {
            MessageBox.Show(this, "Le compagnon est ouvert. Choisissez Quitter dans le menu de son icône près de l'horloge, puis réessayez.", "Compagnon en cours", MessageBoxButtons.OK, MessageBoxIcon.Information);
            return;
        }
        if (complete)
        {
            try
            {
                Process.Start(new ProcessStartInfo(Path.Combine(Program.InstallDirectory, "ForeverPulseCompanion.exe")) { UseShellExecute = true });
                Close();
            }
            catch (Exception error) { MessageBox.Show(this, error.Message, "Ouverture impossible", MessageBoxButtons.OK, MessageBoxIcon.Error); }
            return;
        }
        if (!Program.Windows11())
        {
            MessageBox.Show(this, "Cet installateur nécessite Windows 11 en 64 bits.", "Windows non compatible", MessageBoxButtons.OK, MessageBoxIcon.Error);
            return;
        }
        installing = true;
        install.Enabled = close.Enabled = desktop.Enabled = false;
        progress.Style = ProgressBarStyle.Marquee;
        progress.Visible = true;
        status.Text = "Installation en cours…";
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
                throw new InvalidOperationException("Windows Installer a renvoyé le code " + result + ".\nJournal : " + log);
            string installed = Path.Combine(Program.InstallDirectory, "ForeverPulseCompanion.exe");
            if (Program.Hash(installed) != PackageInfo.CompanionHash)
                throw new InvalidDataException("Le fichier installé ne correspond pas à la version attendue.\nJournal : " + log);
            succeeded = complete = true;
            install.Text = "Ouvrir";
            status.Text = "Installation terminée. Cliquez sur Ouvrir pour lancer la version 0.8.0, ou sur Fermer pour la lancer plus tard.";
        }
        catch (Exception error)
        {
            status.Text = "L'installation n'a pas abouti. Vous pouvez réessayer.";
            MessageBox.Show(this, error.Message, "Installation interrompue", MessageBoxButtons.OK, MessageBoxIcon.Error);
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
